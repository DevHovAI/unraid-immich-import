// Package fakeimmich is a fake Immich for tests.
package fakeimmich

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

type Asset struct {
	ID, Name, Checksum string
	CreatedAt          time.Time
}

type Server struct {
	*httptest.Server
	mu        sync.Mutex
	Assets    map[string]*Asset   // by checksum
	Albums    map[string][]string // name → IDs
	Stacks    [][]string
	Fail      map[string]bool // file names whose upload fails with 500
	Delay     time.Duration   // per upload
	Uploads   int
	Checked   int
	nextID    int
	albumName map[string]string    // ID → name
	albumTime map[string]time.Time // ID → last change
}

func New() *Server {
	s := &Server{Assets: map[string]*Asset{}, Albums: map[string][]string{}, Fail: map[string]bool{},
		albumName: map[string]string{}, albumTime: map[string]time.Time{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Have adds a file as if it had been uploaded earlier.
func (s *Server) Have(name string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := Sum(content)
	s.nextID++
	s.Assets[sum] = &Asset{ID: fmt.Sprintf("old-%d", s.nextID), Name: name, Checksum: sum,
		CreatedAt: time.Now().Add(-24 * time.Hour)}
}

func Sum(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != "KEY" {
		w.WriteHeader(401)
		return
	}
	js := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/server/about":
		js(200, map[string]string{"version": "v3.2.2"})

	case r.Method == "POST" && r.URL.Path == "/api/assets/bulk-upload-check":
		var in struct {
			Assets []struct{ ID, Checksum string } `json:"assets"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		s.Checked += len(in.Assets)
		var out []map[string]any
		for _, a := range in.Assets {
			if ex, ok := s.Assets[a.Checksum]; ok {
				out = append(out, map[string]any{"id": a.ID, "action": "reject", "reason": "duplicate", "assetId": ex.ID})
			} else {
				out = append(out, map[string]any{"id": a.ID, "action": "accept"})
			}
		}
		s.mu.Unlock()
		js(200, map[string]any{"results": out})

	case r.Method == "POST" && r.URL.Path == "/api/assets":
		f, hdr, err := r.FormFile("assetData")
		if err != nil {
			js(400, map[string]string{"message": err.Error()})
			return
		}
		if r.FormValue("fileCreatedAt") == "" {
			js(400, map[string]string{"message": "fileCreatedAt missing"})
			return
		}
		if s.Delay > 0 {
			select {
			case <-time.After(s.Delay):
			case <-r.Context().Done():
				return
			}
		}
		b, _ := io.ReadAll(f)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.Uploads++
		if s.Fail[hdr.Filename] {
			s.mu.Unlock()
			js(500, map[string]string{"message": "broken"})
			s.mu.Lock()
			return
		}
		sum := Sum(b)
		if ex, ok := s.Assets[sum]; ok {
			js(200, map[string]string{"id": ex.ID, "status": "duplicate"})
			return
		}
		s.nextID++
		a := &Asset{ID: fmt.Sprintf("new-%d", s.nextID), Name: hdr.Filename, Checksum: sum, CreatedAt: time.Now()}
		s.Assets[sum] = a
		js(201, map[string]string{"id": a.ID, "status": "created"})

	case r.Method == "GET" && r.URL.Path == "/api/albums":
		s.mu.Lock()
		var out []map[string]any
		for id, name := range s.albumName {
			out = append(out, map[string]any{"id": id, "albumName": name, "updatedAt": s.albumTime[id]})
		}
		s.mu.Unlock()
		js(200, out)

	case r.Method == "POST" && r.URL.Path == "/api/albums":
		var in struct{ AlbumName string }
		json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		id := fmt.Sprintf("album-%d", len(s.albumName)+1)
		s.albumName[id] = in.AlbumName
		s.albumTime[id] = time.Now()
		s.Albums[in.AlbumName] = nil
		s.mu.Unlock()
		js(201, map[string]string{"id": id})

	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/albums/"):
		id := strings.Split(r.URL.Path, "/")[3]
		var in struct{ IDs []string }
		json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		name := s.albumName[id]
		s.albumTime[id] = time.Now()
		s.Albums[name] = append(s.Albums[name], in.IDs...)
		s.mu.Unlock()
		js(200, []any{})

	case r.Method == "POST" && r.URL.Path == "/api/search/metadata":
		var in struct{ CreatedAfter *time.Time }
		json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		var items []map[string]any
		for _, a := range s.Assets {
			if in.CreatedAfter != nil && !a.CreatedAt.After(*in.CreatedAfter) {
				continue
			}
			items = append(items, map[string]any{"id": a.ID, "type": "IMAGE", "originalFileName": a.Name,
				"createdAt": a.CreatedAt, "exifInfo": map[string]any{"dateTimeOriginal": "2026-10-01T12:00:00Z", "make": "SONY", "model": "X"}})
		}
		s.mu.Unlock()
		js(200, map[string]any{"assets": map[string]any{"items": items, "nextPage": nil}})

	case r.Method == "POST" && r.URL.Path == "/api/stacks":
		var in struct{ AssetIDs []string }
		json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		s.Stacks = append(s.Stacks, in.AssetIDs)
		s.mu.Unlock()
		js(201, map[string]string{"id": "stack"})

	default:
		js(404, map[string]string{"message": "unknown " + r.Method + " " + r.URL.Path})
	}
}

// Lock/Unlock for tests that read fields
func (s *Server) Lock()   { s.mu.Lock() }
func (s *Server) Unlock() { s.mu.Unlock() }
