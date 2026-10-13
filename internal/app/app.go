// Package app connects watcher, importer, stacking and Telegram bot.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	MemoryFile = "imported.txt"
	LastFile   = "last.txt"
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

	// StackRetryDelay: how long to wait until Immich has read the metadata
	StackRetryDelay time.Duration

	ctx       context.Context // lives as long as Run
	mu        sync.Mutex
	answer    chan string // open while waiting for the album name
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
		StackRetryDelay: 30 * time.Minute}
	a.Bot = telegram.New(cfg.TGToken, cfg.TGChat, log.Printf)
	a.Imp = &importer.Importer{
		Opt:    importer.Options{DryRun: cfg.DryRun, Parallel: cfg.Parallel, ProgressSteps: cfg.ProgressSteps},
		Immich: a.Immich, Mem: mem, Notify: a.notify, Logf: log.Printf,
		ChooseAlbum: a.chooseAlbum,
	}
	return a
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
	ver, err := a.Immich.About(ctx)
	start := fmt.Sprintf("✅ camera-import v%s started%s", a.Version, a.mode())
	if err != nil {
		start = fmt.Sprintf("⚠️ camera-import v%s started, but Immich (%s) is not responding: %v",
			a.Version, a.Cfg.ImmichURL, err)
	} else {
		start += fmt.Sprintf(" (Immich %s, %d files in memory)", ver, a.Mem.Len())
	}
	a.notify(start)

	if a.Bot.Enabled() {
		if err := a.Bot.SetCommands(ctx, commands); err != nil {
			a.Log.Printf("set Telegram commands: %v", err)
		}
		go a.Bot.Poll(ctx, a.handle)
	}
	a.watch(ctx)
}

// watch looks for cards at regular intervals. A card is imported once, and
// again only after it was gone.
func (a *App) watch(ctx context.Context) {
	done := map[string]bool{}
	t := time.NewTicker(a.Cfg.PollInterval)
	defer t.Stop()
	for {
		present := map[string]bool{}
		for _, c := range importer.FindCards(a.Cfg.DisksDir) {
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
	case res.Total == 0:
		msg = fmt.Sprintf("📷 %s: nothing new (%d files already imported). The card can be removed.", c.Label, res.Known)
	default:
		msg = summary(res, a.mode())
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
	if !r.Canceled {
		b.WriteString("\nThe card can be removed.")
	}
	return b.String()
}

func (a *App) lastPath() string { return filepath.Join(a.Cfg.ConfigDir, LastFile) }

func (a *App) saveLast(msg string) {
	_ = os.WriteFile(a.lastPath(), []byte(time.Now().Format("2006-01-02 15:04")+"\n"+msg+"\n"), 0o644)
}

// chooseAlbum picks the album name according to ALBUM_MODE; asks via
// Telegram for "ask".
func (a *App) chooseAlbum(ctx context.Context, card string, n int) string {
	def := "Import " + time.Now().Format("2006-01-02")
	switch a.Cfg.AlbumMode {
	case "fixed":
		return a.Cfg.Album
	case "date":
		return def
	case "ask":
	default:
		return ""
	}
	ch := make(chan string, 1)
	a.mu.Lock()
	a.answer = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.answer = nil
		a.mu.Unlock()
	}()
	a.notify(fmt.Sprintf("📷 Card detected: %d new files on %s%s\nWhich album? Reply with a name or /none.\nNo reply within %d min: “%s”",
		n, card, a.mode(), int(a.Cfg.AlbumAskTimeout.Minutes()), def))
	select {
	case ans := <-ch:
		return ans
	case <-time.After(a.Cfg.AlbumAskTimeout):
		return def
	case <-ctx.Done():
		return ""
	}
}

// answerAlbum passes the answer on to a pending album question.
func (a *App) answerAlbum(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answer == nil {
		return false
	}
	select {
	case a.answer <- name:
	default:
	}
	a.answer = nil
	return true
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
