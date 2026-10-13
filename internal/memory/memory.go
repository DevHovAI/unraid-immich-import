// Package memory remembers which files have already arrived in Immich.
// The key is name + size + modification time: a stat is enough for that, the
// file itself does not have to be read when the card is inserted again.
package memory

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"
)

type Store struct {
	mu   sync.Mutex
	path string
	f    *os.File
	set  map[string]struct{}
}

func Key(name string, size int64, mtime time.Time) string {
	return fmt.Sprintf("%s\t%d\t%d", name, size, mtime.Unix())
}

// Open loads the file (one line per key) and appends new entries.
func Open(path string) (*Store, error) {
	s := &Store{path: path, set: map[string]struct{}{}}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				s.set[line] = struct{}{}
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

func (s *Store) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[key]
	return ok
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.set)
}

// Add remembers the keys and writes them to disk right away, so a crash or
// restart forgets nothing that was confirmed.
func (s *Store) Add(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := bufio.NewWriter(s.f)
	for _, k := range keys {
		if _, ok := s.set[k]; ok {
			continue
		}
		s.set[k] = struct{}{}
		w.WriteString(k)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return s.f.Sync()
}

// Reset forgets everything (command /forget).
func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Truncate(0); err != nil {
		return err
	}
	s.set = map[string]struct{}{}
	return nil
}

func (s *Store) Close() error { return s.f.Close() }
