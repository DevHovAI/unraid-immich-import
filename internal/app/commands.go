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
	{Name: "status", Description: "Fortschritt von Import und Stapeln"},
	{Name: "abbrechen", Description: "laufenden Import oder Stapeln stoppen"},
	{Name: "letzter", Description: "Zusammenfassung des letzten Imports"},
	{Name: "log", Description: "letzte Zeilen des Protokolls"},
	{Name: "stapeln", Description: "RAW+HIF/JPG-Paare stapeln (optional: JJJJ-MM-TT)"},
	{Name: "kein", Description: "beim Albumnamen: kein Album"},
	{Name: "vergessen", Description: "Gedächtnis löschen: Karte wird wieder ganz geprüft"},
	{Name: "hilfe", Description: "alle Befehle"},
}

const help = `Befehle:
/status – Fortschritt von Import und Stapeln
/abbrechen – laufenden Import (sonst Stapeln) stoppen
/letzter – Zusammenfassung des letzten Imports
/log – letzte Zeilen des Protokolls
/stapeln – RAW+HIF/JPG-Paare der ganzen Bibliothek stapeln
/stapeln JJJJ-MM-TT – nur seit diesem Tag Hochgeladenes
/kein – beim Albumnamen: kein Album
/vergessen – vergessen, was schon importiert ist (z.B. um in Immich gelöschte Bilder neu zu holen)`

// handle verarbeitet eine Nachricht aus dem eigenen Chat.
func (a *App) handle(text string) {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(cmd, "/") {
		// Antwort auf die Albumfrage
		if a.answerAlbum(text) {
			a.notify("📁 Album: " + text)
		}
		return
	}
	cmd, _, _ = strings.Cut(cmd, "@") // "/status@MeinBot" in Gruppen
	switch cmd {
	case "/status":
		a.Bot.Send(a.statusText())
	case "/abbrechen", "/cancel":
		switch {
		case a.Imp.Cancel():
			a.answerAlbum("") // eine offene Albumfrage beenden
			a.Bot.Send("⏹️ Import wird abgebrochen …")
		case a.cancelStack():
			a.Bot.Send("⏹️ Stapeln wird abgebrochen …")
		default:
			a.Bot.Send("💤 Weder Import noch Stapeln aktiv.")
		}
	case "/letzter":
		if b, err := os.ReadFile(a.lastPath()); err == nil {
			a.Bot.Send(string(b))
		} else {
			a.Bot.Send("Noch kein Import abgeschlossen.")
		}
	case "/log":
		a.Bot.Send("📄 Protokoll:\n" + a.Log.Tail(15, 250))
	case "/stapeln":
		var since *time.Time
		if arg != "" {
			t, err := time.ParseInLocation("2006-01-02", arg, time.Local)
			if err != nil {
				a.Bot.Send("Unbekanntes Datum: " + arg + " (Format JJJJ-MM-TT)")
				return
			}
			since = &t
		}
		scope := "ganze Bibliothek"
		if since != nil {
			scope = "seit " + arg
		}
		a.Bot.Send("🗂️ Stapeln gestartet (" + scope + ") …")
		go a.runStack(a.ctx, since, 0, false)
	case "/kein":
		if a.answerAlbum("") {
			a.Bot.Send("📁 Kein Album.")
		}
	case "/vergessen":
		if a.Imp.Current() != nil {
			a.Bot.Send("⏳ Erst nach dem laufenden Import.")
			return
		}
		n := a.Mem.Len()
		if err := a.Mem.Reset(); err != nil {
			a.Bot.Send("❌ Gedächtnis löschen fehlgeschlagen: " + err.Error())
			return
		}
		a.Bot.Send(fmt.Sprintf("🧹 %d Einträge vergessen. Beim nächsten Einstecken wird die ganze Karte geprüft; was Immich schon hat, wird nicht erneut hochgeladen.", n))
	case "/start", "/hilfe", "/help":
		a.Bot.Send(help)
	default:
		a.Bot.Send("Unbekannter Befehl. /hilfe zeigt alle.")
	}
}

func (a *App) statusText() string {
	var parts []string
	if p := a.Imp.Current(); p != nil {
		phase := p.Phase()
		s := fmt.Sprintf("📊 Import läuft%s (%s): %s\n", a.mode(), p.Card, phase)
		if phase == "prüft auf Duplikate" {
			s += fmt.Sprintf("Geprüft: %d von %d neuen Dateien\n", p.Checked.Load(), p.Total)
		}
		s += fmt.Sprintf("Erledigt: %d von %d Dateien (%d %%)\n  davon neu hochgeladen: %d\n  davon schon vorhanden: %d\nFehler: %d\nLaufzeit: %s",
			p.Done.Load(), p.Total, p.Percent(), p.Created.Load(), p.Dup.Load(), p.Failed.Load(),
			importer.FmtDur(time.Since(p.Start)))
		if p.Known > 0 {
			s += fmt.Sprintf("\n%d Dateien schon früher importiert, übersprungen", p.Known)
		}
		if p.Album != "" {
			s += "\nAlbum: " + p.Album
		}
		parts = append(parts, s)
	}
	a.mu.Lock()
	if st := a.stackInfo; st != nil {
		parts = append(parts, fmt.Sprintf("🗂️ Stapeln (%s) läuft seit %s\n%s",
			st.scope, importer.FmtDur(time.Since(st.start)), st.progress))
	}
	a.mu.Unlock()
	if len(parts) == 0 {
		return fmt.Sprintf("💤 Weder Import noch Stapeln aktiv.\n%d Dateien im Gedächtnis.", a.Mem.Len())
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
