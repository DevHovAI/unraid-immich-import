package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/fakeimmich"
	"github.com/DevHovAI/unraid-immich-import/internal/immich"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// card legt eine Karte mit Dateien (Name → Inhalt) an.
func card(t *testing.T, files map[string]string) Card {
	root := filepath.Join(t.TempDir(), "SONY")
	for name, content := range files {
		p := filepath.Join(root, "DCIM", "100MSDCF", name)
		if strings.HasSuffix(name, ".MP4") {
			p = filepath.Join(root, "PRIVATE", "M4ROOT", "CLIP", name)
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Card{Label: "SONY", Root: root}
}

type env struct {
	srv  *fakeimmich.Server
	mem  *memory.Store
	im   *Importer
	msgs []string
	mu   sync.Mutex
}

func setup(t *testing.T, album string) *env {
	e := &env{srv: fakeimmich.New()}
	t.Cleanup(e.srv.Close)
	var err error
	e.mem, err = memory.Open(filepath.Join(t.TempDir(), "mem.txt"))
	if err != nil {
		t.Fatal(err)
	}
	e.im = &Importer{
		Opt:         Options{Parallel: 2},
		Immich:      immich.New(e.srv.URL, "KEY"),
		Mem:         e.mem,
		Notify:      func(s string) { e.mu.Lock(); e.msgs = append(e.msgs, s); e.mu.Unlock() },
		Logf:        t.Logf,
		ChooseAlbum: func(context.Context, string, int) string { return album },
	}
	return e
}

func TestImportRememberAndRetry(t *testing.T) {
	e := setup(t, "Urlaub")
	c := card(t, map[string]string{
		"DSC00001.ARW": "raw1", "DSC00001.HIF": "hif1", // schon in Immich
		"DSC00002.ARW": "raw2", "DSC00002.HIF": "hif2", "C0001.MP4": "video",
		"notes.txt": "ignorieren", "._DSC00002.ARW": "mac-müll",
	})
	e.srv.Have("DSC00001.ARW", []byte("raw1"))
	e.srv.Have("DSC00001.HIF", []byte("hif1"))
	e.srv.Fail["DSC00002.HIF"] = true

	res, err := e.im.Import(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 5 || res.Created != 2 || res.Dup != 2 || res.Failed != 1 || res.Known != 0 {
		t.Fatalf("erster Lauf: %+v", res)
	}
	if got := e.srv.Albums["Urlaub"]; len(got) != 2 {
		t.Fatalf("Album soll nur die 2 neuen enthalten: %v", got)
	}
	if e.mem.Len() != 4 {
		t.Fatalf("Gedächtnis: %d statt 4 (Fehlschlag darf nicht rein)", e.mem.Len())
	}

	// Zweites Einstecken: nur die fehlgeschlagene Datei wird erneut versucht
	delete(e.srv.Fail, "DSC00002.HIF")
	e.srv.Checked = 0
	res, err = e.im.Import(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if res.Known != 4 || res.Total != 1 || res.Created != 1 || e.srv.Checked != 1 {
		t.Fatalf("zweiter Lauf: %+v, geprüft %d", res, e.srv.Checked)
	}
	if got := e.srv.Albums["Urlaub"]; len(got) != 3 {
		t.Fatalf("vorhandenes Album weiterverwenden: %v", e.srv.Albums)
	}

	// Drittes Mal: nichts Neues, Immich wird gar nicht gefragt
	e.srv.Checked = 0
	res, _ = e.im.Import(context.Background(), c)
	if res.Total != 0 || res.Known != 5 || e.srv.Checked != 0 {
		t.Fatalf("dritter Lauf: %+v", res)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	e := setup(t, "")
	e.im.Opt.DryRun = true
	c := card(t, map[string]string{"DSC00001.ARW": "a", "DSC00001.HIF": "b"})
	e.srv.Have("DSC00001.HIF", []byte("b"))
	res, err := e.im.Import(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Dup != 1 || e.srv.Uploads != 0 || e.mem.Len() != 0 {
		t.Fatalf("%+v, Uploads %d, Gedächtnis %d", res, e.srv.Uploads, e.mem.Len())
	}
}

func TestCancel(t *testing.T) {
	e := setup(t, "")
	e.im.Opt.Parallel = 1
	e.srv.Delay = 200 * time.Millisecond
	files := map[string]string{}
	for _, n := range []string{"A", "B", "C", "D", "E", "F"} {
		files["DSC_"+n+".JPG"] = n
	}
	c := card(t, files)
	go func() {
		for e.im.Current() == nil || e.im.Current().Created.Load() == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		if !e.im.Cancel() {
			t.Error("Cancel ohne laufenden Import")
		}
	}()
	res, err := e.im.Import(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Canceled || res.Created == 0 || res.Created >= 6 || res.Failed != 0 {
		t.Fatalf("%+v", res)
	}
	if e.mem.Len() != res.Created {
		t.Fatalf("Gedächtnis %d, hochgeladen %d", e.mem.Len(), res.Created)
	}
	if e.im.Current() != nil || e.im.Cancel() {
		t.Fatal("nach dem Import darf nichts mehr laufen")
	}
}

func TestFindCards(t *testing.T) {
	disks := t.TempDir()
	os.MkdirAll(filepath.Join(disks, "SONY", "DCIM"), 0o755)
	os.MkdirAll(filepath.Join(disks, "BACKUP", "daten"), 0o755)
	cards := FindCards(disks)
	if len(cards) != 1 || cards[0].Label != "SONY" {
		t.Fatalf("%+v", cards)
	}
}
