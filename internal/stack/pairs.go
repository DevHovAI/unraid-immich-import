// Package stack stapelt zusammengehörige RAW- und Bild-Dateien in Immich
// (z.B. DSC01234.ARW + DSC01234.HIF), auch für früher hochgeladene Bilder.
//
// Ein Paar entsteht nur, wenn
//   - der Dateiname ohne Endung gleich ist,
//   - die Aufnahmezeiten höchstens die Toleranz auseinanderliegen,
//   - das Kameramodell gleich ist (wenn beide eins haben),
//   - die Zuordnung eindeutig ist (genau ein Partner auf beiden Seiten),
//   - keine der beiden Dateien schon in einem Stapel liegt.
package stack

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/immich"
)

var rawExt = set("arw", "cr2", "cr3", "nef", "nrw", "raf", "dng", "orf", "rw2",
	"pef", "srw", "3fr", "iiq", "erf", "srf", "sr2")
var displayExt = set("hif", "heif", "heic", "jpg", "jpeg")

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

type file struct {
	id, name string
	raw      bool
	taken    *time.Time // nur das von Immich ausgelesene Aufnahmedatum
	camera   string
	stacked  bool
}

// Collector sammelt Dateien seitenweise, damit nicht die ganze Bibliothek als
// Asset-Strukturen im Speicher liegt.
type Collector struct {
	groups map[string][]file
	Seen   int
}

func NewCollector() *Collector { return &Collector{groups: map[string][]file{}} }

func (c *Collector) Add(assets []immich.Asset) {
	for _, a := range assets {
		c.Seen++
		if a.Type != "IMAGE" || a.IsTrashed {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(a.OriginalFileName), "."))
		if !rawExt[ext] && !displayExt[ext] {
			continue
		}
		f := file{id: a.ID, name: a.OriginalFileName, raw: rawExt[ext], stacked: a.Stack != nil}
		if a.ExifInfo != nil {
			f.taken = a.ExifInfo.DateTimeOriginal
			f.camera = strings.TrimSpace(a.ExifInfo.Make + " " + a.ExifInfo.Model)
		}
		base := strings.ToLower(strings.TrimSuffix(a.OriginalFileName, filepath.Ext(a.OriginalFileName)))
		c.groups[base] = append(c.groups[base], f)
	}
}

type Pair struct {
	Primary, Other         string // Asset-IDs, Primary liegt vorne
	PrimaryName, OtherName string
}

type Stats struct {
	Ambiguous int // mehrere mögliche Partner
	NoDate    int // Dateinamen, bei denen Immich das Aufnahmedatum noch fehlt
}

func matches(r, i file, tol time.Duration) bool {
	if r.taken == nil || i.taken == nil {
		return false
	}
	d := r.taken.Sub(*i.taken)
	if d < 0 {
		d = -d
	}
	if d > tol {
		return false
	}
	return r.camera == "" || i.camera == "" || r.camera == i.camera
}

// Pairs bildet die Paare. cover "raw" legt die RAW-Datei nach vorne.
func (c *Collector) Pairs(tol time.Duration, cover string) ([]Pair, Stats) {
	var pairs []Pair
	var st Stats
	for _, files := range c.groups {
		var raws, imgs []file
		undated := false
		for _, f := range files {
			if f.raw {
				raws = append(raws, f)
			} else {
				imgs = append(imgs, f)
			}
			if f.taken == nil && !f.stacked {
				undated = true
			}
		}
		if len(raws) == 0 || len(imgs) == 0 {
			continue
		}
		// Ohne Aufnahmedatum lässt sich nicht sicher paaren; später erneut versuchen
		if undated {
			st.NoDate++
			continue
		}
		for _, r := range raws {
			var cand []file
			for _, i := range imgs {
				if matches(r, i, tol) {
					cand = append(cand, i)
				}
			}
			if len(cand) != 1 {
				if len(cand) > 1 {
					st.Ambiguous++
				}
				continue
			}
			i := cand[0]
			// Gegenprobe: das Bild darf nur zu diesem einen RAW passen
			back := 0
			for _, x := range raws {
				if matches(x, i, tol) {
					back++
				}
			}
			if back != 1 {
				st.Ambiguous++
				continue
			}
			if r.stacked || i.stacked {
				continue
			}
			p := Pair{Primary: i.id, PrimaryName: i.name, Other: r.id, OtherName: r.name}
			if cover == "raw" {
				p = Pair{Primary: r.id, PrimaryName: r.name, Other: i.id, OtherName: i.name}
			}
			pairs = append(pairs, p)
		}
	}
	return pairs, st
}
