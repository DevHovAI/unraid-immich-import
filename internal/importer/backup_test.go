package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// withBackup turns the backup on, into a fresh folder.
func withBackup(t *testing.T, e *env) *Backup {
	bm, err := memory.Open(filepath.Join(t.TempDir(), "backed-up.txt"))
	if err != nil {
		t.Fatal(err)
	}
	b := &Backup{Dir: t.TempDir(), Mem: bm}
	e.im.Backup, e.im.BackupOn = b, func() bool { return true }
	return b
}

func setTime(t *testing.T, c Card, name string, ts time.Time) {
	if err := os.Chtimes(filepath.Join(c.Root, "DCIM", "100MSDCF", name), ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestBackupByDayAndOnlyOnce(t *testing.T) {
	e := setup(t, "")
	b := withBackup(t, e)
	c := card(t, map[string]string{"DSC00001.ARW": "raw1", "DSC00002.HIF": "hif2"})
	day := time.Date(2026, 10, 16, 12, 0, 0, 0, time.Local)
	setTime(t, c, "DSC00001.ARW", day)
	setTime(t, c, "DSC00002.HIF", day.AddDate(0, 0, 1))

	res, err := e.im.Import(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if bk := res.Backup; bk.Total != 2 || bk.Copied != 2 || bk.Failed != 0 || res.Created != 2 {
		t.Fatalf("%+v", res)
	}
	got, err := os.ReadFile(filepath.Join(b.Dir, "2026", "2026-10-16", "DSC00001.ARW"))
	if err != nil || string(got) != "raw1" {
		t.Fatalf("%q %v", got, err)
	}
	st, _ := os.Stat(filepath.Join(b.Dir, "2026", "2026-10-17", "DSC00002.HIF"))
	if st == nil || !st.ModTime().Equal(day.AddDate(0, 0, 1)) {
		t.Fatalf("copy keeps the time: %v", st)
	}
	left, _ := filepath.Glob(filepath.Join(b.Dir, "*", "*", ".*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}

	// Second time: nothing to do
	res, _ = e.im.Import(context.Background(), c)
	if res.Total != 0 || res.Backup.Total != 0 || res.Backup.Known != 2 {
		t.Fatalf("second run: %+v", res)
	}
}

// Turning the backup on later also copies files that were imported before.
func TestBackupOfImportedFiles(t *testing.T) {
	e := setup(t, "")
	c := card(t, map[string]string{"DSC00001.ARW": "raw1"})
	if _, err := e.im.Import(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	withBackup(t, e)
	res, _ := e.im.Import(context.Background(), c)
	if res.Total != 0 || res.Backup.Copied != 1 {
		t.Fatalf("%+v", res)
	}
}

// Same name, other content (the camera's counter wrapped): keep both.
func TestBackupNameClash(t *testing.T) {
	e := setup(t, "")
	b := withBackup(t, e)
	c := card(t, map[string]string{"DSC00001.ARW": "new photo"})
	day := time.Date(2026, 10, 16, 12, 0, 0, 0, time.Local)
	setTime(t, c, "DSC00001.ARW", day)
	dir := filepath.Join(b.Dir, "2026", "2026-10-16")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "DSC00001.ARW"), []byte("old"), 0o644)

	if _, err := e.im.Import(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "DSC00001_1.ARW"))
	old, _ := os.ReadFile(filepath.Join(dir, "DSC00001.ARW"))
	if string(got) != "new photo" || string(old) != "old" {
		t.Fatalf("%q %q", got, old)
	}
}

// Without a mapped folder nothing is created and the import still runs.
func TestBackupFolderMissing(t *testing.T) {
	e := setup(t, "")
	b := withBackup(t, e)
	b.Dir = filepath.Join(b.Dir, "missing")
	c := card(t, map[string]string{"DSC00001.ARW": "raw1"})
	res, _ := e.im.Import(context.Background(), c)
	if res.Backup.Failed != 1 || res.Created != 1 || len(res.Backup.Errors) != 1 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(b.Dir); err == nil {
		t.Fatal("backup folder must not be created")
	}
	if b.Mem.Len() != 0 {
		t.Fatal("nothing may be remembered as backed up")
	}
}

func TestBackupDryRun(t *testing.T) {
	e := setup(t, "")
	e.im.Opt.DryRun = true
	b := withBackup(t, e)
	c := card(t, map[string]string{"DSC00001.ARW": "raw1"})
	res, _ := e.im.Import(context.Background(), c)
	if res.Backup.Copied != 1 || b.Mem.Len() != 0 {
		t.Fatalf("%+v", res)
	}
	if left, _ := os.ReadDir(b.Dir); len(left) != 0 {
		t.Fatalf("dry run wrote %v", left)
	}
}
