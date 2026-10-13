package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPersistAndReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "imported.txt")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	k1 := Key("DSC00001.ARW", 100, time.Unix(1700000000, 0))
	k2 := Key("DSC00001.HIF", 50, time.Unix(1700000000, 0))
	if err := s.Add(k1, k2, k1); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Has(k1) || !s.Has(k2) || s.Len() != 2 {
		t.Fatalf("after restart: Len=%d", s.Len())
	}
	if s.Has(Key("DSC00001.ARW", 101, time.Unix(1700000000, 0))) {
		t.Fatal("a different size must not count as known")
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	s.Add(k2)
	s.Close()
	s, _ = Open(p)
	if s.Has(k1) || !s.Has(k2) {
		t.Fatal("Reset had no effect")
	}
}
