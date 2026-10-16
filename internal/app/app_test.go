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
	"github.com/DevHovAI/unraid-immich-import/internal/importer"
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

// waitFor waits until a message with this text arrived and returns it.
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
	tb.Fatalf("no message with %q, sent:\n%s", part, strings.Join(t.sent, "\n---\n"))
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
	mem, _ := memory.Open(filepath.Join(dir, MemoryFile))
	a := New(cfg, "test", NewLogger(filepath.Join(dir, "log"), io.Discard), mem)
	a.Bot.API = bot.URL
	a.UnmountWait = 400 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	bot.waitFor(t, "camera-import vtest started (Immich v3.2.2")

	// insert the card
	root := filepath.Join(disks, "SONY")
	os.MkdirAll(filepath.Join(root, "DCIM", "100MSDCF"), 0o755)
	for name, c := range map[string]string{"DSC00001.ARW": "raw", "DSC00001.HIF": "hif"} {
		os.WriteFile(filepath.Join(root, "DCIM", "100MSDCF", name), []byte(c), 0o644)
	}
	bot.waitFor(t, "Which album?")
	if s := a.statusText(); !strings.Contains(s, "waiting for the album name") {
		t.Fatalf("/status: %s", s)
	}
	a.handle("Autumn")
	sum := bot.waitFor(t, "✅ Import done")
	if !strings.Contains(sum, "helper script camera-import-helper is not running") {
		t.Fatalf("without helper: %s", sum)
	}
	if !strings.Contains(sum, "2 new") || !strings.Contains(sum, "Album: Autumn (2 newly added)") {
		t.Fatalf("summary: %s", sum)
	}
	bot.waitFor(t, "1 of 1 pairs stacked")
	srv.Lock()
	if len(srv.Stacks) != 1 || len(srv.Albums["Autumn"]) != 2 {
		t.Fatalf("stacks %v, albums %v", srv.Stacks, srv.Albums)
	}
	srv.Unlock()

	// remove and insert again: nothing new, no album question
	os.Rename(root, root+".gone")
	time.Sleep(1500 * time.Millisecond)
	os.Rename(root+".gone", root)
	bot.waitFor(t, "nothing new (2 files already imported)")

	a.handle("/forget")
	bot.waitFor(t, "Forgot 2 entries")
	a.handle("/stack 12.10.2026")
	bot.waitFor(t, "Unknown date")
	a.handle("/status")
	bot.waitFor(t, "Neither import nor stacking running")
	a.handle("/last")
	bot.waitFor(t, "nothing new (2")
	a.handle("/hilfe") // German alias from v1
	bot.waitFor(t, "Commands:")
}

func TestBackupCommand(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"IMMICH_URL": "http://x:2283", "IMMICH_API_KEY": "KEY",
			"CONFIG_DIR": dir, "BACKUP_DIR": backup}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := memory.Open(filepath.Join(dir, MemoryFile))
	log := NewLogger(filepath.Join(dir, "log"), io.Discard)
	a := New(cfg, "test", log, mem)

	if s := a.backupCommand("on"); !strings.Contains(s, "stays off") || a.backupOn.Load() {
		t.Fatalf("without folder: %s", s)
	}
	os.Mkdir(backup, 0o755)
	if s := a.backupCommand("true"); !strings.Contains(s, "Backup on") {
		t.Fatalf("%s", s)
	}
	if s := a.backupCommand(""); !strings.Contains(s, "Backup is on") {
		t.Fatalf("%s", s)
	}
	// survives a restart
	if !New(cfg, "test", log, mem).backupOn.Load() {
		t.Fatal("state lost after restart")
	}
	a.backupCommand("off")
	if New(cfg, "test", log, mem).backupOn.Load() {
		t.Fatal("still on")
	}
	if s := a.backupCommand("maybe"); !strings.Contains(s, "/backup on") {
		t.Fatalf("%s", s)
	}
}

func TestUnmount(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(func(k string) string {
		return map[string]string{"IMMICH_URL": "http://x:2283", "IMMICH_API_KEY": "KEY", "CONFIG_DIR": dir}[k]
	})
	mem, _ := memory.Open(filepath.Join(dir, MemoryFile))
	a := New(cfg, "test", NewLogger(filepath.Join(dir, "log"), io.Discard), mem)
	a.UnmountWait = 2 * time.Second
	root := filepath.Join(dir, "disks", "SONY")
	os.MkdirAll(filepath.Join(root, "DCIM"), 0o755)
	c := importer.Card{Label: "SONY", Root: root}

	// helper: takes the request and unmounts
	req := filepath.Join(dir, UnmountDir, "SONY")
	go func() {
		for range 100 {
			if _, err := os.Stat(req); err == nil {
				os.Remove(req)
				os.RemoveAll(root)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if s := a.unmount(c); !strings.Contains(s, "Card unmounted") {
		t.Fatal(s)
	}

	// helper takes the request, but the card stays
	os.MkdirAll(filepath.Join(root, "DCIM"), 0o755)
	a.UnmountWait = 300 * time.Millisecond
	go func() {
		for range 100 {
			if os.Remove(req) == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	if s := a.unmount(c); !strings.Contains(s, "Unmounting failed") {
		t.Fatal(s)
	}

	// turned off
	a.Cfg.AutoUnmount = false
	if s := a.unmount(c); !strings.HasPrefix(s, "Unmount it in") {
		t.Fatal(s)
	}
	if _, err := os.Stat(req); err == nil {
		t.Fatal("no request when turned off")
	}
}
