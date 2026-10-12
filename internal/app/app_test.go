package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/config"
	"github.com/DevHovAI/unraid-immich-import/internal/fakeimmich"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

type tg struct {
	*httptest.Server
	mu   sync.Mutex
	sent []string
}

func newTG() *tg {
	t := &tg{}
	t.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.mu.Lock()
			t.sent = append(t.sent, r.Form.Get("text"))
			t.mu.Unlock()
		}
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			time.Sleep(50 * time.Millisecond)
			io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	return t
}

// waitFor wartet, bis eine Nachricht mit diesem Text kam, und gibt sie zurück.
func (t *tg) waitFor(tb testing.TB, part string) string {
	tb.Helper()
	for i := 0; i < 300; i++ {
		t.mu.Lock()
		for _, m := range t.sent {
			if strings.Contains(m, part) {
				t.mu.Unlock()
				return m
			}
		}
		t.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	tb.Fatalf("keine Nachricht mit %q, gesendet:\n%s", part, strings.Join(t.sent, "\n---\n"))
	return ""
}

func TestEndToEnd(t *testing.T) {
	srv := fakeimmich.New()
	defer srv.Close()
	bot := newTG()
	defer bot.Close()

	dir := t.TempDir()
	disks := filepath.Join(dir, "disks")
	os.MkdirAll(disks, 0o755)
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"IMMICH_URL": srv.URL, "IMMICH_API_KEY": "KEY", "TG_BOT_TOKEN": "T", "TG_CHAT_ID": "1",
			"DISKS_DIR": disks, "CONFIG_DIR": dir, "POLL_INTERVAL": "1", "PROGRESS_STEPS": "",
		}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := memory.Open(filepath.Join(dir, "importiert.txt"))
	a := New(cfg, "test", NewLogger(filepath.Join(dir, "log"), io.Discard), mem)
	a.Bot.API = bot.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	bot.waitFor(t, "camera-import vtest gestartet (Immich v3.2.2")

	// Karte einstecken
	root := filepath.Join(disks, "SONY")
	os.MkdirAll(filepath.Join(root, "DCIM", "100MSDCF"), 0o755)
	for name, c := range map[string]string{"DSC00001.ARW": "raw", "DSC00001.HIF": "hif"} {
		os.WriteFile(filepath.Join(root, "DCIM", "100MSDCF", name), []byte(c), 0o644)
	}
	bot.waitFor(t, "In welches Album?")
	if s := a.statusText(); !strings.Contains(s, "wartet auf den Albumnamen") {
		t.Fatalf("/status: %s", s)
	}
	a.handle("Herbst")
	sum := bot.waitFor(t, "✅ Import fertig")
	if !strings.Contains(sum, "2 neu") || !strings.Contains(sum, "Album: Herbst (2 neu hinzugefügt)") {
		t.Fatalf("Zusammenfassung: %s", sum)
	}
	bot.waitFor(t, "1 von 1 Paaren gestapelt")
	srv.Lock()
	if len(srv.Stacks) != 1 || len(srv.Albums["Herbst"]) != 2 {
		t.Fatalf("Stapel %v, Alben %v", srv.Stacks, srv.Albums)
	}
	srv.Unlock()

	// Abziehen und wieder einstecken: nichts Neues, keine Albumfrage
	os.Rename(root, root+".weg")
	time.Sleep(1500 * time.Millisecond)
	os.Rename(root+".weg", root)
	bot.waitFor(t, "nichts Neues (2 Dateien schon importiert)")

	a.handle("/vergessen")
	bot.waitFor(t, "2 Einträge vergessen")
	a.handle("/stapeln 12.10.2026")
	bot.waitFor(t, "Unbekanntes Datum")
	a.handle("/status")
	bot.waitFor(t, "Weder Import noch Stapeln aktiv")
	a.handle("/letzter")
	bot.waitFor(t, "nichts Neues (2")
}
