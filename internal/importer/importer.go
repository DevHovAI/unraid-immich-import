// Package importer erkennt eingesteckte Karten und lädt neue Dateien nach Immich.
package importer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/immich"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

type Options struct {
	DryRun        bool
	Parallel      int
	ProgressSteps []int
}

// Importer lädt jeweils eine Karte hoch. Status und Abbrechen sind von anderen
// Goroutinen (dem Bot) aus aufrufbar.
type Importer struct {
	Opt    Options
	Immich *immich.Client
	Mem    *memory.Store
	Notify func(string)
	Logf   func(string, ...any)
	// ChooseAlbum fragt den Albumnamen ("" = keins) für n neue Dateien.
	ChooseAlbum func(ctx context.Context, card string, n int) string

	mu     sync.Mutex
	cur    *Progress
	cancel context.CancelFunc
}

type Progress struct {
	Card     string
	Album    string
	Start    time.Time
	Total    int   // neue Dateien
	Known    int   // vom Gedächtnis übersprungen
	Bytes    int64 // Größe der neuen Dateien
	phase    atomic.Value
	Checked  atomic.Int64 // Prüfsumme berechnet
	Done     atomic.Int64 // fertig (hochgeladen, Duplikat oder Fehler)
	Created  atomic.Int64
	Dup      atomic.Int64
	Failed   atomic.Int64
	BytesUp  atomic.Int64
	Rejected atomic.Int64 // von Immich nicht unterstütztes Format
}

func (p *Progress) Phase() string     { s, _ := p.phase.Load().(string); return s }
func (p *Progress) setPhase(s string) { p.phase.Store(s) }

// Percent nach Bytes, damit große Videos richtig zählen
func (p *Progress) Percent() int {
	if p.Bytes == 0 {
		return 100
	}
	return int(100 * p.BytesUp.Load() / p.Bytes)
}

type Result struct {
	Card       string
	Album      string
	Start      time.Time
	Duration   time.Duration
	Total      int // neue Dateien auf der Karte
	Known      int // vom Gedächtnis übersprungen
	Created    int
	Dup        int
	Failed     int
	Rejected   int
	Trashed    int // liegt in Immich im Papierkorb
	Canceled   bool
	Errors     []string
	CreatedIDs []string
}

// Current gibt den laufenden Import zurück oder nil.
func (im *Importer) Current() *Progress {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.cur
}

// Cancel bricht den laufenden Import ab; false, wenn keiner läuft.
func (im *Importer) Cancel() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.cancel == nil {
		return false
	}
	im.cancel()
	return true
}

func (im *Importer) logf(format string, args ...any) {
	if im.Logf != nil {
		im.Logf(format, args...)
	}
}

type job struct {
	f   File
	sum string
}

// Import lädt alle neuen Dateien der Karte hoch. Bekannte Dateien (Gedächtnis)
// werden nicht angefasst, nur per stat erkannt.
func (im *Importer) Import(ctx context.Context, card Card) (Result, error) {
	res := Result{Card: card.Label, Start: time.Now()}
	files, err := Scan(card)
	if err != nil {
		return res, fmt.Errorf("Karte lesen: %w", err)
	}
	var fresh []File
	var bytes int64
	for _, f := range files {
		if im.Mem.Has(f.Key()) {
			res.Known++
			continue
		}
		fresh = append(fresh, f)
		bytes += f.Size
	}
	res.Total = len(fresh)
	im.logf("%s: %d Dateien, davon %d neu (%s)", card.Label, len(files), len(fresh), fmtBytes(bytes))
	if len(fresh) == 0 {
		return res, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := &Progress{Card: card.Label, Start: res.Start, Total: len(fresh), Known: res.Known, Bytes: bytes}
	p.setPhase("wartet auf den Albumnamen")
	im.mu.Lock()
	im.cur, im.cancel = p, cancel
	im.mu.Unlock()
	defer func() {
		im.mu.Lock()
		im.cur, im.cancel = nil, nil
		im.mu.Unlock()
	}()

	if im.ChooseAlbum != nil {
		res.Album = im.ChooseAlbum(ctx, card.Label, len(fresh))
		p.Album = res.Album
	}
	mode := ""
	if im.Opt.DryRun {
		mode = " (Probelauf)"
	}
	msg := fmt.Sprintf("📷 Import gestartet%s: %d neue Dateien von %s (%s)", mode, len(fresh), card.Label, fmtBytes(bytes))
	if res.Known > 0 {
		msg += fmt.Sprintf(", %d schon früher importiert", res.Known)
	}
	if res.Album != "" {
		msg += fmt.Sprintf("\nAlbum: %s", res.Album)
	}
	im.Notify(msg + "\nBefehle: /status /abbrechen /log")

	stopWatch := im.watchProgress(p)
	defer stopWatch()

	var mu sync.Mutex // schützt res
	fail := func(f File, err error) {
		p.Failed.Add(1)
		p.Done.Add(1)
		mu.Lock()
		res.Failed++
		if len(res.Errors) < 5 {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", f.Name, err))
		}
		mu.Unlock()
		im.logf("Fehler bei %s: %v", f.Path, err)
	}
	remember := func(f File) {
		if im.Opt.DryRun {
			return
		}
		if err := im.Mem.Add(f.Key()); err != nil {
			im.logf("Gedächtnis schreiben: %v", err)
		}
	}

	// 1. Prüfsummen und Duplikatprüfung, in Blöcken: so beginnt der Upload
	//    nicht erst, wenn die ganze Karte gelesen ist
	p.setPhase("prüft auf Duplikate")
	jobs := make(chan job)
	var wg sync.WaitGroup
	for range max(im.Opt.Parallel, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				im.upload(ctx, p, j, &res, &mu, fail, remember)
			}
		}()
	}

	const block = 50
	for start := 0; start < len(fresh) && ctx.Err() == nil; start += block {
		chunk := fresh[start:min(start+block, len(fresh))]
		var items []job
		var check []immich.CheckItem
		for _, f := range chunk {
			sum, err := sha1File(ctx, f.Path)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				fail(f, err)
				continue
			}
			p.Checked.Add(1)
			check = append(check, immich.CheckItem{ID: strconv.Itoa(len(items)), Checksum: sum})
			items = append(items, job{f: f, sum: sum})
		}
		if ctx.Err() != nil || len(items) == 0 {
			continue
		}
		results, err := im.Immich.BulkCheck(ctx, check)
		if err != nil {
			if ctx.Err() == nil {
				for _, j := range items {
					fail(j.f, fmt.Errorf("Duplikatprüfung: %w", err))
				}
			}
			continue
		}
		byID := map[string]immich.CheckResult{}
		for _, r := range results {
			byID[r.ID] = r
		}
		for i, j := range items {
			r := byID[strconv.Itoa(i)]
			switch {
			case r.Action == "reject" && r.Reason == "duplicate":
				p.Dup.Add(1)
				p.Done.Add(1)
				p.BytesUp.Add(j.f.Size)
				mu.Lock()
				res.Dup++
				if r.IsTrashed {
					res.Trashed++
				}
				mu.Unlock()
				remember(j.f)
			case r.Action == "reject":
				p.Rejected.Add(1)
				p.Done.Add(1)
				p.BytesUp.Add(j.f.Size)
				mu.Lock()
				res.Rejected++
				mu.Unlock()
				im.logf("%s: von Immich abgelehnt (%s)", j.f.Name, r.Reason)
				remember(j.f) // wird nie klappen, nicht jedes Mal neu versuchen
			case im.Opt.DryRun:
				p.Created.Add(1) // im Probelauf: „würde hochladen“
				p.Done.Add(1)
				p.BytesUp.Add(j.f.Size)
				mu.Lock()
				res.Created++
				mu.Unlock()
			default:
				select {
				case jobs <- j:
					p.setPhase("lädt hoch")
				case <-ctx.Done():
				}
			}
		}
	}
	close(jobs)
	wg.Wait()

	res.Canceled = ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)
	res.Duration = time.Since(res.Start)

	// 2. Nur die wirklich neuen Dateien ins Album
	if res.Album != "" && len(res.CreatedIDs) > 0 {
		p.setPhase("legt ins Album")
		if _, err := im.Immich.AddToAlbum(context.WithoutCancel(ctx), res.Album, res.CreatedIDs); err != nil {
			res.Errors = append(res.Errors, "Album: "+err.Error())
			im.logf("Album %q: %v", res.Album, err)
		}
	}
	return res, nil
}

func (im *Importer) upload(ctx context.Context, p *Progress, j job, res *Result, mu *sync.Mutex,
	fail func(File, error), remember func(File)) {
	var sent atomic.Int64
	r, err := im.Immich.Upload(ctx, j.f.Path, j.sum, func(n int64) {
		sent.Add(n)
		p.BytesUp.Add(n)
	})
	if err != nil {
		p.BytesUp.Add(j.f.Size - sent.Load()) // für den Fortschritt als erledigt zählen
		if ctx.Err() != nil {
			return // abgebrochen: nicht als Fehler zählen, nächstes Mal neu
		}
		fail(j.f, err)
		return
	}
	p.Done.Add(1)
	mu.Lock()
	if r.Status == "duplicate" {
		res.Dup++
		p.Dup.Add(1)
	} else {
		res.Created++
		res.CreatedIDs = append(res.CreatedIDs, r.ID)
		p.Created.Add(1)
	}
	mu.Unlock()
	remember(j.f)
}

// watchProgress meldet Zwischenstände bei den Prozentmarken aus PROGRESS_STEPS.
// Springt der Fortschritt über mehrere Marken, kommt nur die höchste.
func (im *Importer) watchProgress(p *Progress) (stop func()) {
	steps := append([]int(nil), im.Opt.ProgressSteps...)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for len(steps) > 0 {
			select {
			case <-done:
				return
			case <-t.C:
			}
			pct, hit := p.Percent(), 0
			for len(steps) > 0 && pct >= steps[0] {
				hit, steps = steps[0], steps[1:]
			}
			if hit > 0 {
				im.Notify(fmt.Sprintf("⏳ %d %% geschafft (%s): %d von %d Dateien, Laufzeit %s",
					hit, p.Card, p.Done.Load(), p.Total, FmtDur(time.Since(p.Start))))
			}
		}
	}()
	return func() { close(done) }
}

func sha1File(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func FmtDur(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%dh %02dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dm %02ds", s/60, s%60)
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/(1<<30)), ".", ",", 1)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}
