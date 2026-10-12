// Package app verbindet Watcher, Importer, Stapeln und Telegram-Bot.
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

type App struct {
	Cfg     config.Config
	Version string
	Immich  *immich.Client
	Mem     *memory.Store
	Bot     *telegram.Bot
	Log     *Logger
	Imp     *importer.Importer

	// StackRetryDelay: Wartezeit, bis Immich die Metadaten gelesen hat
	StackRetryDelay time.Duration

	ctx       context.Context // lebt so lange wie Run
	mu        sync.Mutex
	answer    chan string // offen, solange auf den Albumnamen gewartet wird
	stackMu   sync.Mutex  // nur ein Stapellauf gleichzeitig
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
		return " (Probelauf)"
	}
	return ""
}

// Run läuft bis ctx endet: Bot und Kartenüberwachung.
func (a *App) Run(ctx context.Context) {
	a.ctx = ctx
	ver, err := a.Immich.About(ctx)
	start := fmt.Sprintf("✅ camera-import v%s gestartet%s", a.Version, a.mode())
	if err != nil {
		start = fmt.Sprintf("⚠️ camera-import v%s gestartet, aber Immich (%s) antwortet nicht: %v",
			a.Version, a.Cfg.ImmichURL, err)
	} else {
		start += fmt.Sprintf(" (Immich %s, %d Dateien im Gedächtnis)", ver, a.Mem.Len())
	}
	a.notify(start)

	if a.Bot.Enabled() {
		if err := a.Bot.SetCommands(ctx, commands); err != nil {
			a.Log.Printf("Telegram-Befehle setzen: %v", err)
		}
		go a.Bot.Poll(ctx, a.handle)
	}
	a.watch(ctx)
}

// watch schaut regelmäßig nach Karten. Eine Karte wird einmal importiert und
// erst wieder, nachdem sie verschwunden war.
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
				a.Log.Printf("%s entfernt", filepath.Base(root))
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
	a.Log.Printf("Karte erkannt: %s", c.Root)
	res, err := a.Imp.Import(ctx, c)
	var msg string
	switch {
	case err != nil:
		msg = fmt.Sprintf("❌ Import von %s fehlgeschlagen: %v", c.Label, err)
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			msg += "\nDer Container darf die Karte nicht lesen – siehe README (Unassigned Devices)."
		}
	case res.Total == 0:
		msg = fmt.Sprintf("📷 %s: nichts Neues (%d Dateien schon importiert). Karte kann abgezogen werden.", c.Label, res.Known)
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
		fmt.Fprintf(&b, "⏹️ Import abgebrochen (%s): %d neu, %d schon vorhanden, nach %s", r.Card, r.Created, r.Dup, importer.FmtDur(r.Duration))
	case r.Failed == 0:
		verb := "neu"
		if mode != "" {
			verb = "würden hochgeladen"
		}
		fmt.Fprintf(&b, "✅ Import fertig%s (%s): %d %s, %d schon vorhanden, Dauer %s", mode, r.Card, r.Created, verb, r.Dup, importer.FmtDur(r.Duration))
	default:
		fmt.Fprintf(&b, "⚠️ Import mit Fehlern (%s): %d neu, %d schon vorhanden, %d Fehler – beim nächsten Einstecken werden sie erneut versucht", r.Card, r.Created, r.Dup, r.Failed)
	}
	if r.Known > 0 {
		fmt.Fprintf(&b, "\n%d Dateien schon früher importiert, übersprungen", r.Known)
	}
	if r.Trashed > 0 {
		fmt.Fprintf(&b, "\n%d davon liegen in Immich im Papierkorb", r.Trashed)
	}
	if r.Rejected > 0 {
		fmt.Fprintf(&b, "\n%d Dateien lehnt Immich ab (Format nicht unterstützt)", r.Rejected)
	}
	if r.Album != "" {
		fmt.Fprintf(&b, "\nAlbum: %s (%d neu hinzugefügt)", r.Album, len(r.CreatedIDs))
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "\n• %s", e)
	}
	if !r.Canceled {
		b.WriteString("\nKarte kann abgezogen werden.")
	}
	return b.String()
}

func (a *App) lastPath() string { return filepath.Join(a.Cfg.ConfigDir, "letzter.txt") }

func (a *App) saveLast(msg string) {
	_ = os.WriteFile(a.lastPath(), []byte(time.Now().Format("2006-01-02 15:04")+"\n"+msg+"\n"), 0o644)
}

// chooseAlbum bestimmt den Albumnamen nach ALBUM_MODE; bei "ask" per Telegram.
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
	a.notify(fmt.Sprintf("📷 Karte erkannt: %d neue Dateien von %s%s\nIn welches Album? Antworte mit dem Namen oder /kein.\nOhne Antwort in %d Min: „%s“",
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

// answerAlbum gibt die Antwort an eine laufende Albumfrage weiter.
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

// runStack stapelt RAW+Bild-Paare. Fehlt Immich noch das Aufnahmedatum, wird
// es nach StackRetryDelay erneut versucht, solange retries übrig sind.
// auto: nach einem Import gestartet – ohne Paare dann keine eigene Meldung.
func (a *App) runStack(ctx context.Context, since *time.Time, retries int, auto bool) {
	if !a.stackMu.TryLock() {
		if retries > 0 {
			time.AfterFunc(a.StackRetryDelay, func() { a.runStack(ctx, since, retries, auto) })
		} else {
			a.notify("🗂️ Stapeln läuft bereits.")
		}
		return
	}
	defer a.stackMu.Unlock()

	scope := "ganze Bibliothek"
	if since != nil {
		scope = "seit " + since.Local().Format("2006-01-02 15:04")
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := &stackState{start: time.Now(), scope: scope, progress: "wird gestartet …", cancel: cancel}
	a.mu.Lock()
	a.stackInfo = st
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.stackInfo = nil
		a.mu.Unlock()
	}()

	a.Log.Printf("Stapeln (%s) gestartet", scope)
	res, err := stack.Run(sctx, a.Immich, since, a.Cfg.StackTolerance, a.Cfg.StackCover, a.Cfg.DryRun,
		func(s string) {
			a.mu.Lock()
			st.progress = s
			a.mu.Unlock()
		})
	if err != nil {
		if sctx.Err() != nil {
			a.notify(fmt.Sprintf("⏹️ Stapeln (%s) abgebrochen: %d Stapel angelegt", scope, res.Created))
		} else {
			a.notify(fmt.Sprintf("❌ Stapeln (%s) fehlgeschlagen: %v", scope, err))
		}
		return
	}
	msg := fmt.Sprintf("🗂️ Stapeln (%s)%s: %d von %d Paaren gestapelt", scope, a.mode(), res.Created, res.Pairs)
	if a.Cfg.DryRun {
		msg = fmt.Sprintf("🗂️ Stapeln (%s, Probelauf): %d Paare gefunden", scope, res.Pairs)
	}
	if res.Ambiguous > 0 {
		msg += fmt.Sprintf("\n%d mehrdeutig, übersprungen (z.B. HIF+JPG+ARW mit gleichem Namen)", res.Ambiguous)
	}
	if res.Errors > 0 {
		msg += fmt.Sprintf("\n❌ %d Fehler:\n%s", res.Errors, strings.Join(res.ErrorSamples, "\n"))
	}
	if res.NoDate > 0 {
		if retries > 0 {
			msg += fmt.Sprintf("\n%d Dateinamen noch ohne Aufnahmedatum (Immich liest Metadaten noch) – neuer Versuch in %d Min.",
				res.NoDate, int(a.StackRetryDelay.Minutes()))
			time.AfterFunc(a.StackRetryDelay, func() { a.runStack(ctx, since, retries-1, auto) })
		} else {
			msg += fmt.Sprintf("\n%d Dateinamen ohne Aufnahmedatum – später mit /stapeln erneut versuchen.", res.NoDate)
		}
	}
	if auto && res.Pairs == 0 && res.NoDate == 0 && res.Errors == 0 {
		a.Log.Printf("%s", msg)
		return
	}
	a.notify(msg)
}
