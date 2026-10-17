package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/importer"
	"github.com/DevHovAI/unraid-immich-import/internal/telegram"
)

var commands = []telegram.Command{
	{Name: "status", Description: "progress of import and stacking"},
	{Name: "cancel", Description: "stop the running import or stacking"},
	{Name: "last", Description: "summary of the last import"},
	{Name: "log", Description: "last lines of the log"},
	{Name: "stack", Description: "stack RAW+HIF/JPG pairs (optional: YYYY-MM-DD)"},
	{Name: "none", Description: "when asked for an album: no album"},
	{Name: "switch_album", Description: "change the album of the running import"},
	{Name: "forget", Description: "clear the memory: the card is checked in full again"},
	{Name: "backup", Description: "back up the card now; on/off: always; status"},
	{Name: "help", Description: "all commands"},
}

// aliases maps the German commands of v1 to the English ones, so they keep
// working. They are not listed anywhere.
var aliases = map[string]string{
	"/abbrechen": "/cancel",
	"/letzter":   "/last",
	"/stapeln":   "/stack",
	"/kein":      "/none",
	"/vergessen": "/forget",
	"/hilfe":     "/help",
}

const help = `Commands:
/status – progress of import and stacking
/cancel – stop the running import (otherwise stacking)
/last – summary of the last import
/log – last lines of the log
/stack – stack RAW+HIF/JPG pairs across the whole library
/stack YYYY-MM-DD – only what was uploaded since that day
/none – when asked for an album: no album
/switch_album – change the album of the running import (buttons, or /switch_album <name>)
/forget – forget what was already imported (e.g. to fetch photos deleted in Immich again)
/backup – back up the inserted card now (or the next one)
/backup on|off – back up every card before the import
/backup status – state and folder`

// handle processes a message from our own chat.
func (a *App) handle(text string) {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(cmd, "/") {
		// answer to the album question
		if a.answerAlbum(text) {
			a.closePicker("📁 Album: " + text)
		}
		return
	}
	cmd, _, _ = strings.Cut(cmd, "@") // "/status@MyBot" in groups
	if c, ok := aliases[cmd]; ok {
		cmd = c
	}
	switch cmd {
	case "/status":
		a.Bot.Send(a.statusText())
	case "/cancel":
		switch {
		case a.Imp.Cancel():
			a.answerAlbum("") // end an open album question
			a.Bot.Send("⏹️ Cancelling import …")
		case a.cancelStack():
			a.Bot.Send("⏹️ Cancelling stacking …")
		default:
			a.Bot.Send("💤 Neither import nor stacking running.")
		}
	case "/last":
		if b, err := os.ReadFile(a.lastPath()); err == nil {
			a.Bot.Send(string(b))
		} else {
			a.Bot.Send("No import finished yet.")
		}
	case "/log":
		a.Bot.Send("📄 Log:\n" + a.Log.Tail(15, 250))
	case "/stack":
		var since *time.Time
		if arg != "" {
			t, err := time.ParseInLocation("2006-01-02", arg, time.Local)
			if err != nil {
				a.Bot.Send("Unknown date: " + arg + " (format YYYY-MM-DD)")
				return
			}
			since = &t
		}
		scope := "whole library"
		if since != nil {
			scope = "since " + arg
		}
		a.Bot.Send("🗂️ Stacking started (" + scope + ") …")
		go a.runStack(a.ctx, since, 0, false)
	case "/none":
		if a.answerAlbum("") {
			a.closePicker("📁 No album.")
		}
	case "/switch_album":
		a.switchAlbum(arg)
	case "/forget":
		if a.Imp.Current() != nil {
			a.Bot.Send("⏳ Only after the running import.")
			return
		}
		n := a.Mem.Len()
		if err := a.Mem.Reset(); err != nil {
			a.Bot.Send("❌ Clearing the memory failed: " + err.Error())
			return
		}
		a.Bot.Send(fmt.Sprintf("🧹 Forgot %d entries. The next time a card is inserted it is checked in full; anything Immich already has is not uploaded again.", n))
	case "/backup":
		a.Bot.Send(a.backupCommand(arg))
	case "/start", "/help":
		a.Bot.Send(help)
	default:
		a.Bot.Send("Unknown command. /help lists them all.")
	}
}

func (a *App) statusText() string {
	var parts []string
	if p := a.Imp.Current(); p != nil {
		phase := p.Phase()
		if p.Total == 0 && p.BackupTotal > 0 {
			parts = append(parts, fmt.Sprintf("💾 Backup running%s (%s): %d of %d files copied, %d errors\nRunning for: %s",
				a.mode(), p.Card, p.BackupDone.Load(), p.BackupTotal, p.BackupFailed.Load(),
				importer.FmtDur(time.Since(p.Start))))
		}
		s := fmt.Sprintf("📊 Import running%s (%s): %s\n", a.mode(), p.Card, phase)
		if phase == importer.PhaseChecking {
			s += fmt.Sprintf("Checked: %d of %d new files\n", p.Checked.Load(), p.Total)
		}
		s += fmt.Sprintf("Done: %d of %d files (%d %%)\n  newly uploaded: %d\n  already there: %d\nErrors: %d\nRunning for: %s",
			p.Done.Load(), p.Total, p.Percent(), p.Created.Load(), p.Dup.Load(), p.Failed.Load(),
			importer.FmtDur(time.Since(p.Start)))
		if p.Known > 0 {
			s += fmt.Sprintf("\n%d files imported earlier, skipped", p.Known)
		}
		if album := p.Album(); album != "" {
			s += "\nAlbum: " + album
		}
		if p.BackupTotal > 0 {
			s += fmt.Sprintf("\n💾 Backup: %d of %d files copied, %d errors",
				p.BackupDone.Load(), p.BackupTotal, p.BackupFailed.Load())
		}
		if p.Total > 0 || p.BackupTotal == 0 {
			parts = append(parts, s)
		}
	}
	a.mu.Lock()
	if st := a.stackInfo; st != nil {
		parts = append(parts, fmt.Sprintf("🗂️ Stacking (%s) running for %s\n%s",
			st.scope, importer.FmtDur(time.Since(st.start)), st.progress))
	}
	a.mu.Unlock()
	if len(parts) == 0 {
		s := fmt.Sprintf("💤 Neither import nor stacking running.\n%d files in memory.", a.Mem.Len())
		if a.backupOn.Load() {
			s += fmt.Sprintf("\n💾 Backup on, %d files backed up.", a.Backup.Mem.Len())
		}
		return s
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) cancelStack() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stackInfo == nil {
		return false
	}
	a.stackInfo.cancel()
	return true
}

func (a *App) backupCommand(arg string) string {
	if a.Backup == nil {
		return "❌ Backup is not available, see /log."
	}
	switch strings.ToLower(arg) {
	case "":
		return a.backupNow()
	case "status":
		state := "off"
		if a.backupOn.Load() {
			state = "on"
		}
		s := fmt.Sprintf("💾 Automatic backup is %s. Folder: %s, %d files backed up.\n/backup backs up now, /backup on or /backup off",
			state, a.Cfg.BackupDir, a.Backup.Mem.Len())
		if err := a.Backup.Ready(); err != nil {
			s += "\n⚠️ " + err.Error()
		}
		return s
	case "on", "true", "1", "an", "ein", "ja":
		if err := a.Backup.Ready(); err != nil {
			return "❌ Backup stays off: " + err.Error()
		}
		if err := a.setBackup(true); err != nil {
			return "❌ " + err.Error()
		}
		s := "💾 Backup on. From the next card on, everything that is not in " + a.Cfg.BackupDir +
			" yet is copied there first, also files that were imported earlier."
		if a.Imp.Current() != nil {
			s += " The running import is not affected."
		}
		return s
	case "off", "false", "0", "aus", "nein":
		if err := a.setBackup(false); err != nil {
			return "❌ " + err.Error()
		}
		return "💾 Backup off."
	default:
		return "Use /backup (back up now), /backup on, /backup off or /backup status."
	}
}
