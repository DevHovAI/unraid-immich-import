package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFileNames(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "importiert.txt"), []byte("old memory\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "letzter.txt"), []byte("old last\n"), 0o644)
	os.WriteFile(filepath.Join(dir, LastFile), []byte("new last\n"), 0o644)

	MigrateFileNames(dir, NewLogger("", io.Discard))

	if b, err := os.ReadFile(filepath.Join(dir, MemoryFile)); err != nil || string(b) != "old memory\n" {
		t.Fatalf("memory not moved: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "importiert.txt")); !os.IsNotExist(err) {
		t.Fatal("old memory file still there")
	}
	// an existing file under the new name wins
	if b, _ := os.ReadFile(filepath.Join(dir, LastFile)); string(b) != "new last\n" {
		t.Fatalf("last overwritten: %q", b)
	}
}
