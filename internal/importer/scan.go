package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// Extensions that are imported (photos, RAW, videos)
var mediaExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`jpg jpeg hif heif heic arw cr2 cr3 nef nrw raf dng orf rw2
		pef srw 3fr iiq erf srf sr2 mp4 mts m2ts mov`) {
		mediaExt[e] = true
	}
}

type Card struct {
	Label string // name of the mount point, e.g. the volume label
	Root  string
}

// Dirs: DCIM and, for Sony videos, PRIVATE/M4ROOT/CLIP
func (c Card) Dirs() []string {
	dirs := []string{filepath.Join(c.Root, "DCIM")}
	clip := filepath.Join(c.Root, "PRIVATE", "M4ROOT", "CLIP")
	if st, err := os.Stat(clip); err == nil && st.IsDir() {
		dirs = append(dirs, clip)
	}
	return dirs
}

// FindCards returns all mounted volumes with a DCIM folder. seen describes
// every entry of disksDir with the reason it was skipped, so the log can show
// why a mounted card is not picked up. broken lists the entries that could
// not be read at all, e.g. a stale mount after the camera was unplugged.
func FindCards(disksDir string) (cards []Card, seen, broken []string) {
	entries, err := os.ReadDir(disksDir)
	if err != nil {
		return nil, []string{err.Error()}, []string{err.Error()}
	}
	for _, e := range entries {
		root := filepath.Join(disksDir, e.Name())
		st, err := os.Stat(filepath.Join(root, "DCIM"))
		switch {
		case err == nil && st.IsDir():
			cards = append(cards, Card{Label: e.Name(), Root: root})
			seen = append(seen, e.Name()+" (card)")
		case err == nil:
			seen = append(seen, e.Name()+" (DCIM is not a folder)")
		case errors.Is(err, fs.ErrNotExist):
			seen = append(seen, e.Name()+" (no DCIM)")
		default:
			var pe *fs.PathError
			if errors.As(err, &pe) {
				err = pe.Err // the path is already in the name
			}
			seen = append(seen, fmt.Sprintf("%s (%v)", e.Name(), err))
			broken = append(broken, seen[len(seen)-1])
		}
	}
	return cards, seen, broken
}

type File struct {
	Path  string
	Name  string
	Size  int64
	MTime time.Time
}

func (f File) Key() string { return memory.Key(f.Name, f.Size, f.MTime) }

// Scan collects the card's media files without reading them (stat only).
func Scan(c Card) ([]File, error) {
	var files []File
	for _, dir := range c.Dirs() {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() || strings.HasPrefix(name, ".") {
				return nil
			}
			if !mediaExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))] {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			files = append(files, File{Path: p, Name: name, Size: info.Size(), MTime: info.ModTime()})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
