package app

import (
	"context"
	"fmt"
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
	mu      sync.Mutex
	sent    []string // texts, edits as "EDIT <id>: text"
	buttons []string // reply_markup of sent messages
}

func newTG() *tg {
	t := &tg{}
	t.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.mu.Lock()
			t.sent = append(t.sent, r.Form.Get("text"))
			t.buttons = append(t.buttons, r.Form.Get("reply_markup"))
			id := len(t.sent)
			t.mu.Unlock()
			fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			t.mu.Lock()
			t.sent = append(t.sent, "EDIT "+r.Form.Get("message_id")+": "+r.Form.Get("text"))
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
	if s := a.backupCommand("status"); !strings.Contains(s, "Automatic backup is on") {
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

// Album buttons: recent albums are offered, a press answers the question,
// and /switch_album changes the album while the upload runs.
func TestAlbumButtonsAndSwitch(t *testing.T) {
	srv := fakeimmich.New()
	defer srv.Close()
	srv.Delay = 300 * time.Millisecond
	bot := newTG()
	defer bot.Close()

	dir := t.TempDir()
	disks := filepath.Join(dir, "disks")
	os.MkdirAll(disks, 0o755)
	cfg, _ := config.Load(func(k string) string {
		return map[string]string{
			"IMMICH_URL": srv.URL, "IMMICH_API_KEY": "KEY", "TG_BOT_TOKEN": "T", "TG_CHAT_ID": "1",
			"DISKS_DIR": disks, "CONFIG_DIR": dir, "POLL_INTERVAL": "1", "PROGRESS_STEPS": "",
			"STACK_PAIRS": "0", "AUTO_UNMOUNT": "0", "UPLOAD_PARALLEL": "1",
		}[k]
	})
	mem, _ := memory.Open(filepath.Join(dir, MemoryFile))
	a := New(cfg, "test", NewLogger(filepath.Join(dir, "log"), io.Discard), mem)
	a.Bot.API = bot.URL
	for _, n := range []string{"Old trip", "Summer"} {
		if _, err := a.Immich.AddToAlbum(context.Background(), n, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	a.handle("/switch_album")
	bot.waitFor(t, "No import running")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	root := filepath.Join(disks, "SONY")
	os.MkdirAll(filepath.Join(root, "DCIM", "100MSDCF"), 0o755)
	for _, n := range []string{"DSC00001.JPG", "DSC00002.JPG", "DSC00003.JPG"} {
		os.WriteFile(filepath.Join(root, "DCIM", "100MSDCF", n), []byte(n), 0o644)
	}
	bot.waitFor(t, "Which album?")
	bot.mu.Lock()
	kb := bot.buttons[len(bot.buttons)-1]
	bot.mu.Unlock()
	// most recently changed first, date album and "no album" last
	if !strings.Contains(kb, "Summer") || strings.Index(kb, "Summer") > strings.Index(kb, "Old trip") ||
		!strings.Contains(kb, "No album") {
		t.Fatalf("buttons: %s", kb)
	}

	a.mu.Lock()
	seq, names := a.picker.seq, a.picker.names
	a.mu.Unlock()
	if names[2] != "Summer" {
		t.Fatalf("names %q", names)
	}
	a.onButton(fmt.Sprintf("%d:2", seq), 7)
	bot.waitFor(t, "EDIT 7: 📁 Album: Summer")
	bot.waitFor(t, "Import started")

	// while uploading: switch via buttons to "Old trip"
	a.handle("/switch_album")
	bot.waitFor(t, "Album of the running import: Summer")
	a.mu.Lock()
	seq2, names := a.picker.seq, a.picker.names
	a.mu.Unlock()
	a.onButton(fmt.Sprintf("%d:%d", seq, 2), 8) // old message
	bot.waitFor(t, "EDIT 8: This selection is outdated")
	idx := -1
	for i, n := range names {
		if n == "Old trip" {
			idx = i
		}
	}
	a.onButton(fmt.Sprintf("%d:%d", seq2, idx), 9)
	bot.waitFor(t, "EDIT 9: 📁 Switched to album: Old trip")

	sum := bot.waitFor(t, "✅ Import done")
	if !strings.Contains(sum, "Album: Old trip (3 newly added)") {
		t.Fatalf("summary: %s", sum)
	}
	srv.Lock()
	defer srv.Unlock()
	if len(srv.Albums["Old trip"]) != 3 || len(srv.Albums["Summer"]) != 0 {
		t.Fatalf("albums: %v", srv.Albums)
	}
}

// /backup without on/off: now if a card is inserted, otherwise the next card
// once; the automatic backup stays off.
func TestBackupNow(t *testing.T) {
	srv := fakeimmich.New()
	defer srv.Close()
	bot := newTG()
	defer bot.Close()
	dir := t.TempDir()
	disks := filepath.Join(dir, "disks")
	backup := filepath.Join(dir, "backup")
	os.MkdirAll(disks, 0o755)
	cfg, _ := config.Load(func(k string) string {
		return map[string]string{
			"IMMICH_URL": srv.URL, "IMMICH_API_KEY": "KEY", "TG_BOT_TOKEN": "T", "TG_CHAT_ID": "1",
			"DISKS_DIR": disks, "CONFIG_DIR": dir, "BACKUP_DIR": backup, "POLL_INTERVAL": "1",
			"PROGRESS_STEPS": "", "STACK_PAIRS": "0", "AUTO_UNMOUNT": "0", "ALBUM_MODE": "none",
		}[k]
	})
	mem, _ := memory.Open(filepath.Join(dir, MemoryFile))
	a := New(cfg, "test", NewLogger(filepath.Join(dir, "log"), io.Discard), mem)
	a.Bot.API = bot.URL

	if s := a.backupCommand(""); !strings.Contains(s, "No backup possible") {
		t.Fatalf("without folder: %s", s)
	}
	os.Mkdir(backup, 0o755)

	card := func(name, file string) {
		p := filepath.Join(disks, name, "DCIM", "100MSDCF")
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, file), []byte(file), 0o644)
	}
	count := func() int {
		n := 0
		filepath.WalkDir(backup, func(_ string, d os.DirEntry, _ error) error {
			if d != nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n
	}

	// no card: armed for the next one
	if s := a.backupCommand(""); !strings.Contains(s, "next card is backed up once") {
		t.Fatal(s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	card("A", "DSC00001.JPG")
	sum := bot.waitFor(t, "✅ Import done")
	if !strings.Contains(sum, "fully backed up") || count() != 1 {
		t.Fatalf("next card: %s, %d files", sum, count())
	}
	if a.backupOn.Load() || a.backupOnce.Load() {
		t.Fatal("one-off backup must not stay on")
	}

	// card inserted and imported without backup: /backup copies it now
	card("B", "DSC00002.JPG")
	bot.waitFor(t, "(B): 1 new")
	if count() != 1 {
		t.Fatal("B must not be backed up automatically")
	}
	if s := a.backupCommand(""); !strings.Contains(s, "Backing up now") {
		t.Fatal(s)
	}
	bot.waitFor(t, "💾 Backup done (B)")
	if count() != 2 {
		t.Fatalf("%d files", count())
	}
	if len(srv.Albums) != 0 {
		t.Fatalf("backup only must not import: %v", srv.Albums)
	}
	a.backupCommand("")
	bot.waitFor(t, "everything already backed up")

	// during an import: backed up right after it, before unmounting
	srv.Delay = 400 * time.Millisecond
	card("C", "DSC00003.JPG")
	for a.Imp.Current() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if s := a.backupCommand(""); !strings.Contains(s, "right after the running import") {
		t.Fatal(s)
	}
	bot.waitFor(t, "(C): 1 new")
	bot.waitFor(t, "💾 Backup done (C)")
	if count() != 3 {
		t.Fatalf("%d files", count())
	}
}
