// Package app connects watcher, importer, stacking and Telegram bot.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/config"
	"github.com/DevHovAI/unraid-immich-import/internal/immich"
	"github.com/DevHovAI/unraid-immich-import/internal/importer"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
	"github.com/DevHovAI/unraid-immich-import/internal/stack"
	"github.com/DevHovAI/unraid-immich-import/internal/telegram"
)

// Files in the config directory
const (
	MemoryFile       = "imported.txt"
	LastFile         = "last.txt"
	BackupMemoryFile = "backed-up.txt"
	BackupStateFile  = "backup.txt" // "on" or "off", set with /backup
)

// renamed maps the German file names of 2.0.0-beta.1 to the current ones.
var renamed = map[string]string{
	"importiert.txt": MemoryFile,
	"letzter.txt":    LastFile,
}

// MigrateFileNames renames files from 2.0.0-beta.1, so an update keeps the
// memory. A file that already exists under the new name is left alone.
func MigrateFileNames(dir string, log *Logger) {
	for old, cur := range renamed {
		from, to := filepath.Join(dir, old), filepath.Join(dir, cur)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			log.Printf("rename %s to %s: %v", old, cur, err)
			continue
		}
		log.Printf("renamed %s to %s", old, cur)
	}
}

type App struct {
	Cfg     config.Config
	Version string
	Immich  *immich.Client
	Mem     *memory.Store
	Bot     *telegram.Bot
	Log     *Logger
	Imp     *importer.Importer
	Backup  *importer.Backup // nil if its memory could not be opened

	backupOn atomic.Bool

	// StackRetryDelay: how long to wait until Immich has read the metadata
	StackRetryDelay time.Duration
	// UnmountWait: how long to wait for the helper script to unmount a card
	UnmountWait time.Duration

	ctx       context.Context // lives as long as Run
	mu        sync.Mutex
	answer    chan string // open while waiting for the album name
	picker    picker      // the last album buttons sent
	stackMu   sync.Mutex  // only one stacking run at a time
	stackInfo *stackState
}

type stackState struct {
	start    time.Time
	scope    string
	progress string
	cancel   context.CancelFunc
}

func New(cfg config.Config, version string, log *Logger, mem *memory.Store) *App {
	a := &App{Cfg: cfg, Version: version, Log: log, Mem: mem,
		Immich:          immich.New(cfg.ImmichURL, cfg.APIKey),
		StackRetryDelay: 30 * time.Minute, UnmountWait: 40 * time.Second}
	a.Bot = telegram.New(cfg.TGToken, cfg.TGChat, log.Printf)
	a.Imp = &importer.Importer{
		Opt:    importer.Options{DryRun: cfg.DryRun, Parallel: cfg.Parallel, ProgressSteps: cfg.ProgressSteps},
		Immich: a.Immich, Mem: mem, Notify: a.notify, Logf: log.Printf,
		ChooseAlbum: a.chooseAlbum, BackupOn: a.backupOn.Load,
	}
	if bm, err := memory.Open(filepath.Join(cfg.ConfigDir, BackupMemoryFile)); err != nil {
		log.Printf("open backup memory: %v – backup is not available", err)
		a.Imp.BackupOn = nil
	} else {
		a.Backup = &importer.Backup{Dir: cfg.BackupDir, Mem: bm}
		a.Imp.Backup = a.Backup
		b, _ := os.ReadFile(a.backupStatePath())
		a.backupOn.Store(strings.TrimSpace(string(b)) == "on")
	}
	return a
}

func (a *App) backupStatePath() string { return filepath.Join(a.Cfg.ConfigDir, BackupStateFile) }

// setBackup turns the backup on or off and remembers it across restarts.
func (a *App) setBackup(on bool) error {
	state := "off"
	if on {
		state = "on"
	}
	if err := os.WriteFile(a.backupStatePath(), []byte(state+"\n"), 0o644); err != nil {
		return err
	}
	a.backupOn.Store(on)
	return nil
}

func (a *App) notify(msg string) {
	a.Log.Printf("→ %s", strings.SplitN(msg, "\n", 2)[0])
	a.Bot.Send(msg)
}

func (a *App) mode() string {
	if a.Cfg.DryRun {
		return " (dry run)"
	}
	return ""
}

// Run runs until ctx ends: bot and card watcher.
func (a *App) Run(ctx context.Context) {
	a.ctx = ctx
	a.clearUnmountRequests()
	ver, err := a.Immich.About(ctx)
	start := fmt.Sprintf("✅ camera-import v%s started%s", a.Version, a.mode())
	if err != nil {
		start = fmt.Sprintf("⚠️ camera-import v%s started, but Immich (%s) is not responding: %v",
			a.Version, a.Cfg.ImmichURL, err)
	} else {
		start += fmt.Sprintf(" (Immich %s, %d files in memory)", ver, a.Mem.Len())
	}
	if a.backupOn.Load() {
		start += "\n💾 Backup is on: " + a.Cfg.BackupDir
		if err := a.Backup.Ready(); err != nil {
			start += "\n⚠️ " + err.Error()
		}
	}
	a.notify(start)

	if a.Bot.Enabled() {
		if err := a.Bot.SetCommands(ctx, commands); err != nil {
			a.Log.Printf("set Telegram commands: %v", err)
		}
		go a.Bot.Poll(ctx, a.handle, a.onButton)
	}
	a.watch(ctx)
}

// watch looks for cards at regular intervals. A card is imported once, and
// again only after it was gone.
func (a *App) watch(ctx context.Context) {
	done := map[string]bool{}
	t := time.NewTicker(a.Cfg.PollInterval)
	defer t.Stop()
	last, lastBroken := "", ""
	for {
		cards, seen, broken := importer.FindCards(a.Cfg.DisksDir)
		// Log what is mounted whenever it changes: explains a card that is
		// visible on the host but not imported
		if s := strings.Join(seen, ", "); s != last {
			last = s
			if s == "" {
				s = "empty"
			}
			a.Log.Printf("%s: %s", a.Cfg.DisksDir, s)
		}
		if b := strings.Join(broken, ", "); b != lastBroken {
			lastBroken = b
			if b != "" {
				a.notify("⚠️ Cannot read " + b + " under " + a.Cfg.DisksDir +
					". If the camera was unplugged without unmounting: Main → Unassigned Devices → Unmount, then plug it in again.")
			}
		}
		present := map[string]bool{}
		for _, c := range cards {
			present[c.Root] = true
			if done[c.Root] || ctx.Err() != nil {
				continue
			}
			done[c.Root] = true
			a.importCard(ctx, c)
		}
		for root := range done {
			if !present[root] {
				delete(done, root)
				a.Log.Printf("%s removed", filepath.Base(root))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) importCard(ctx context.Context, c importer.Card) {
	a.Log.Printf("card detected: %s", c.Root)
	res, err := a.Imp.Import(ctx, c)
	var msg string
	switch {
	case err != nil:
		msg = fmt.Sprintf("❌ Import from %s failed: %v", c.Label, err)
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			msg += "\nThe container is not allowed to read the card – see README (Unassigned Devices)."
		}
	case res.Total == 0 && res.Backup.Total == 0:
		msg = fmt.Sprintf("📷 %s: nothing new (%d files already imported", c.Label, res.Known)
		if res.Backup.On {
			msg += " and backed up"
		}
		msg += ")."
	default:
		msg = summary(res, a.mode())
	}
	if ctx.Err() == nil {
		msg += "\n" + a.unmount(c)
	}
	a.notify(msg)
	a.saveLast(msg)

	if err == nil && res.Created > 0 && a.Cfg.StackPairs && !a.Cfg.DryRun {
		since := res.Start.Add(-5 * time.Second)
		go a.runStack(ctx, &since, 3, true)
	}
}

func summary(r importer.Result, mode string) string {
	var b strings.Builder
	switch {
	case r.Canceled:
		fmt.Fprintf(&b, "⏹️ Import cancelled (%s): %d new, %d already there, after %s", r.Card, r.Created, r.Dup, importer.FmtDur(r.Duration))
	case r.Failed == 0:
		verb := "new"
		if mode != "" {
			verb = "would be uploaded"
		}
		fmt.Fprintf(&b, "✅ Import done%s (%s): %d %s, %d already there, took %s", mode, r.Card, r.Created, verb, r.Dup, importer.FmtDur(r.Duration))
	default:
		fmt.Fprintf(&b, "⚠️ Import with errors (%s): %d new, %d already there, %d errors – they are retried the next time the card is inserted", r.Card, r.Created, r.Dup, r.Failed)
	}
	if r.Known > 0 {
		fmt.Fprintf(&b, "\n%d files imported earlier, skipped", r.Known)
	}
	if bk := r.Backup; bk.Total > 0 {
		verb := "copied"
		if mode != "" {
			verb = "would be copied"
		}
		switch {
		case bk.Failed == 0 && bk.Copied == bk.Total:
			fmt.Fprintf(&b, "\n💾 Backup: %d %s – the card is fully backed up", bk.Copied, verb)
		case bk.Failed == 0:
			fmt.Fprintf(&b, "\n💾 Backup: %d of %d %s", bk.Copied, bk.Total, verb)
		default:
			fmt.Fprintf(&b, "\n⚠️ Backup: %d %s, %d errors – retried the next time the card is inserted", bk.Copied, verb, bk.Failed)
		}
		for _, e := range bk.Errors {
			fmt.Fprintf(&b, "\n• Backup: %s", e)
		}
	}
	if r.Trashed > 0 {
		fmt.Fprintf(&b, "\n%d of them are in Immich's trash", r.Trashed)
	}
	if r.Rejected > 0 {
		fmt.Fprintf(&b, "\n%d files rejected by Immich (unsupported format)", r.Rejected)
	}
	if r.Album != "" {
		fmt.Fprintf(&b, "\nAlbum: %s (%d newly added)", r.Album, len(r.CreatedIDs))
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "\n• %s", e)
	}
	return b.String()
}

// UnmountDir is where the container asks the helper script (User Script
// camera-import-helper) to unmount a card: one file per card name.
const UnmountDir = "unmount"

// unmount asks the helper script to unmount the card and waits until it is
// gone. Unassigned Devices never unmounts on its own: a card unplugged while
// mounted stays a dead mount and is not mounted again next time.
func (a *App) unmount(c importer.Card) string {
	const manual = "Unmount it in Main → Unassigned Devices before unplugging."
	if !a.Cfg.AutoUnmount {
		return manual
	}
	dir := filepath.Join(a.Cfg.ConfigDir, UnmountDir)
	req := filepath.Join(dir, c.Label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.Log.Printf("unmount request: %v", err)
		return "⚠️ Could not ask for unmounting. " + manual
	}
	if err := os.WriteFile(req, nil, 0o644); err != nil {
		a.Log.Printf("unmount request: %v", err)
		return "⚠️ Could not ask for unmounting. " + manual
	}
	deadline := time.Now().Add(a.UnmountWait)
	for time.Now().Before(deadline) {
		time.Sleep(a.UnmountWait / 80)
		if _, err := os.Stat(filepath.Join(c.Root, "DCIM")); err != nil {
			a.Log.Printf("%s unmounted", c.Label)
			return "⏏️ Card unmounted – you can unplug it."
		}
	}
	if _, err := os.Stat(req); err == nil {
		os.Remove(req)
		a.Log.Printf("unmount %s: helper script did not pick up the request", c.Label)
		return "⚠️ Not unmounted: the helper script camera-import-helper is not running (see README). " + manual
	}
	a.Log.Printf("unmount %s: still mounted after %s", c.Label, a.UnmountWait)
	return "⚠️ Unmounting failed (details in the system log). " + manual
}

// clearUnmountRequests drops requests from before a restart.
func (a *App) clearUnmountRequests() {
	dir := filepath.Join(a.Cfg.ConfigDir, UnmountDir)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

func (a *App) lastPath() string { return filepath.Join(a.Cfg.ConfigDir, LastFile) }

func (a *App) saveLast(msg string) {
	_ = os.WriteFile(a.lastPath(), []byte(time.Now().Format("2006-01-02 15:04")+"\n"+msg+"\n"), 0o644)
}

// runStack stacks RAW+image pairs. If Immich still lacks the capture date,
// it tries again after StackRetryDelay as long as retries are left.
// auto: started after an import – then no message of its own without pairs.
func (a *App) runStack(ctx context.Context, since *time.Time, retries int, auto bool) {
	if !a.stackMu.TryLock() {
		if retries > 0 {
			time.AfterFunc(a.StackRetryDelay, func() { a.runStack(ctx, since, retries, auto) })
		} else {
			a.notify("🗂️ Stacking is already running.")
		}
		return
	}
	defer a.stackMu.Unlock()

	scope := "whole library"
	if since != nil {
		scope = "since " + since.Local().Format("2006-01-02 15:04")
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := &stackState{start: time.Now(), scope: scope, progress: "starting …", cancel: cancel}
	a.mu.Lock()
	a.stackInfo = st
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.stackInfo = nil
		a.mu.Unlock()
	}()

	a.Log.Printf("stacking (%s) started", scope)
	res, err := stack.Run(sctx, a.Immich, since, a.Cfg.StackTolerance, a.Cfg.StackCover, a.Cfg.DryRun,
		func(s string) {
			a.mu.Lock()
			st.progress = s
			a.mu.Unlock()
		})
	if err != nil {
		if sctx.Err() != nil {
			a.notify(fmt.Sprintf("⏹️ Stacking (%s) cancelled: %d stacks created", scope, res.Created))
		} else {
			a.notify(fmt.Sprintf("❌ Stacking (%s) failed: %v", scope, err))
		}
		return
	}
	msg := fmt.Sprintf("🗂️ Stacking (%s)%s: %d of %d pairs stacked", scope, a.mode(), res.Created, res.Pairs)
	if a.Cfg.DryRun {
		msg = fmt.Sprintf("🗂️ Stacking (%s, dry run): %d pairs found", scope, res.Pairs)
	}
	if res.Ambiguous > 0 {
		msg += fmt.Sprintf("\n%d ambiguous, skipped (e.g. HIF+JPG+ARW with the same name)", res.Ambiguous)
	}
	if res.Errors > 0 {
		msg += fmt.Sprintf("\n❌ %d errors:\n%s", res.Errors, strings.Join(res.ErrorSamples, "\n"))
	}
	if res.NoDate > 0 {
		if retries > 0 {
			msg += fmt.Sprintf("\n%d file names still without a capture date (Immich is still reading metadata) – trying again in %d min.",
				res.NoDate, int(a.StackRetryDelay.Minutes()))
			time.AfterFunc(a.StackRetryDelay, func() { a.runStack(ctx, since, retries-1, auto) })
		} else {
			msg += fmt.Sprintf("\n%d file names without a capture date – try again later with /stack.", res.NoDate)
		}
	}
	if auto && res.Pairs == 0 && res.NoDate == 0 && res.Errors == 0 {
		a.Log.Printf("%s", msg)
		return
	}
	a.notify(msg)
}
