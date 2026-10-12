package importer

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// Endungen, die importiert werden (Fotos, RAW, Videos)
var mediaExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`jpg jpeg hif heif heic arw cr2 cr3 nef nrw raf dng orf rw2
		pef srw 3fr iiq erf srf sr2 mp4 mts m2ts mov`) {
		mediaExt[e] = true
	}
}

type Card struct {
	Label string // Name des Einhängepunkts, z.B. die Datenträgerbezeichnung
	Root  string
}

// Dirs: DCIM und bei Sony-Videos PRIVATE/M4ROOT/CLIP
func (c Card) Dirs() []string {
	dirs := []string{filepath.Join(c.Root, "DCIM")}
	clip := filepath.Join(c.Root, "PRIVATE", "M4ROOT", "CLIP")
	if st, err := os.Stat(clip); err == nil && st.IsDir() {
		dirs = append(dirs, clip)
	}
	return dirs
}

// FindCards liefert alle eingehängten Datenträger mit DCIM-Ordner.
func FindCards(disksDir string) []Card {
	entries, err := os.ReadDir(disksDir)
	if err != nil {
		return nil
	}
	var cards []Card
	for _, e := range entries {
		root := filepath.Join(disksDir, e.Name())
		if st, err := os.Stat(filepath.Join(root, "DCIM")); err == nil && st.IsDir() {
			cards = append(cards, Card{Label: e.Name(), Root: root})
		}
	}
	return cards
}

type File struct {
	Path  string
	Name  string
	Size  int64
	MTime time.Time
}

func (f File) Key() string { return memory.Key(f.Name, f.Size, f.MTime) }

// Scan sammelt die Mediendateien der Karte, ohne sie zu lesen (nur stat).
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
