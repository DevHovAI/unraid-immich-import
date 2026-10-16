// Package importer detects inserted cards and uploads new files to Immich.
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/immich"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// Phases shown by /status
const (
	PhaseAlbum     = "waiting for the album name"
	PhaseBackup    = "backing up"
	PhaseChecking  = "checking for duplicates"
	PhaseUploading = "uploading"
	PhaseAddAlbum  = "adding to album"
)

type Options struct {
	DryRun        bool
	Parallel      int
	ProgressSteps []int
}

// Importer uploads one card at a time. Status and cancel can be called from
// other goroutines (the bot).
type Importer struct {
	Opt    Options
	Immich *immich.Client
	Mem    *memory.Store
	Notify func(string)
	Logf   func(string, ...any)
	// ChooseAlbum asks for the album name ("" = none) for n new files.
	ChooseAlbum func(ctx context.Context, card string, n int) string
	// Backup copies the card before the import if BackupOn says so.
	Backup   *Backup
	BackupOn func() bool

	mu     sync.Mutex
	cur    *Progress
	cancel context.CancelFunc
}

type Progress struct {
	Card     string
	Start    time.Time
	Total    int   // new files
	Known    int   // skipped thanks to the memory
	Bytes    int64 // size of the new files
	phase    atomic.Value
	Checked  atomic.Int64 // checksum computed
	Done     atomic.Int64 // finished (uploaded, duplicate or error)
	Created  atomic.Int64
	Dup      atomic.Int64
	Failed   atomic.Int64
	BytesUp  atomic.Int64
	Rejected atomic.Int64 // format not supported by Immich

	BackupTotal  int   // files to back up
	BackupBytes  int64 // their size
	BackupDone   atomic.Int64
	BackupFailed atomic.Int64
	BackupBytesC atomic.Int64 // bytes copied

	album albumState
}

// The album can still change while the import runs (/switch_album): the
// files are only added at the end.
type albumState struct {
	mu     sync.Mutex
	name   string
	locked bool // files are being added, too late to change
}

func (p *Progress) Album() string {
	p.album.mu.Lock()
	defer p.album.mu.Unlock()
	return p.album.name
}

// SetAlbum changes the album; false if the files are already being added.
func (p *Progress) SetAlbum(name string) bool {
	p.album.mu.Lock()
	defer p.album.mu.Unlock()
	if p.album.locked {
		return false
	}
	p.album.name = name
	return true
}

func (p *Progress) lockAlbum() string {
	p.album.mu.Lock()
	defer p.album.mu.Unlock()
	p.album.locked = true
	return p.album.name
}

func (p *Progress) Phase() string     { s, _ := p.phase.Load().(string); return s }
func (p *Progress) setPhase(s string) { p.phase.Store(s) }

// Percent by bytes over backup and upload, so large videos count correctly
func (p *Progress) Percent() int {
	total := p.Bytes + p.BackupBytes
	if total == 0 {
		return 100
	}
	return int(100 * (p.BytesUp.Load() + p.BackupBytesC.Load()) / total)
}

type Result struct {
	Card       string
	Album      string
	Start      time.Time
	Duration   time.Duration
	Total      int // new files on the card
	Known      int // skipped thanks to the memory
	Created    int
	Dup        int
	Failed     int
	Rejected   int
	Trashed    int // in Immich's trash
	Canceled   bool
	Errors     []string
	CreatedIDs []string

	Backup BackupResult
}

type BackupResult struct {
	On     bool
	Total  int // files that were not backed up yet
	Known  int // backed up earlier
	Copied int
	Failed int
	Errors []string
}

// Current returns the running import or nil.
func (im *Importer) Current() *Progress {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.cur
}

// Cancel stops the running import; false if none is running.
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

// Import backs up (if turned on) and uploads all new files on the card.
// Known files (memory) are not touched, only recognised via stat.
func (im *Importer) Import(ctx context.Context, card Card) (Result, error) {
	res := Result{Card: card.Label, Start: time.Now()}
	files, err := Scan(card)
	if err != nil {
		return res, fmt.Errorf("read card: %w", err)
	}
	var fresh, toBackup []File
	var bytes, backupBytes int64
	for _, f := range files {
		if im.Mem.Has(f.Key()) {
			res.Known++
		} else {
			fresh = append(fresh, f)
			bytes += f.Size
		}
	}
	if im.BackupOn != nil && im.BackupOn() {
		res.Backup.On = true
		for _, f := range files {
			if im.Backup.Mem.Has(f.Key()) {
				res.Backup.Known++
			} else {
				toBackup = append(toBackup, f)
				backupBytes += f.Size
			}
		}
		res.Backup.Total = len(toBackup)
	}
	res.Total = len(fresh)
	im.logf("%s: %d files, %d of them new (%s), %d to back up (%s)", card.Label, len(files),
		len(fresh), fmtBytes(bytes), len(toBackup), fmtBytes(backupBytes))
	if len(fresh) == 0 && len(toBackup) == 0 {
		return res, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := &Progress{Card: card.Label, Start: res.Start, Total: len(fresh), Known: res.Known, Bytes: bytes,
		BackupTotal: len(toBackup), BackupBytes: backupBytes}
	p.setPhase(PhaseAlbum)
	im.mu.Lock()
	im.cur, im.cancel = p, cancel
	im.mu.Unlock()
	defer func() {
		im.mu.Lock()
		im.cur, im.cancel = nil, nil
		im.mu.Unlock()
	}()

	if im.ChooseAlbum != nil && len(fresh) > 0 {
		res.Album = im.ChooseAlbum(ctx, card.Label, len(fresh))
		p.SetAlbum(res.Album)
	}
	mode := ""
	if im.Opt.DryRun {
		mode = " (dry run)"
	}
	msg := fmt.Sprintf("📷 Import started%s: %d new files from %s (%s)", mode, len(fresh), card.Label, fmtBytes(bytes))
	if len(fresh) == 0 {
		msg = fmt.Sprintf("📷 Nothing new to import from %s%s", card.Label, mode)
	}
	if res.Known > 0 {
		msg += fmt.Sprintf(", %d imported earlier", res.Known)
	}
	if len(toBackup) > 0 {
		msg += fmt.Sprintf("\n💾 Backup first: %d files (%s)", len(toBackup), fmtBytes(backupBytes))
	}
	if res.Album != "" {
		msg += fmt.Sprintf("\nAlbum: %s", res.Album)
	}
	im.Notify(msg + "\nCommands: /status /cancel /log")

	stopWatch := im.watchProgress(p)
	defer stopWatch()

	sums := im.backup(ctx, p, toBackup, &res.Backup)
	if len(fresh) > 0 && ctx.Err() == nil {
		im.upload(ctx, p, fresh, sums, &res)
	}
	res.Canceled = ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)
	res.Duration = time.Since(res.Start)

	// Only the really new files go into the album
	res.Album = p.lockAlbum()
	if res.Album != "" && len(res.CreatedIDs) > 0 {
		p.setPhase(PhaseAddAlbum)
		if _, err := im.Immich.AddToAlbum(context.WithoutCancel(ctx), res.Album, res.CreatedIDs); err != nil {
			res.Errors = append(res.Errors, "Album: "+err.Error())
			im.logf("Album %q: %v", res.Album, err)
		}
	}
	return res, nil
}

// backup copies files one by one (the card is the bottleneck) and returns
// their checksums, so the upload does not read them a second time.
func (im *Importer) backup(ctx context.Context, p *Progress, files []File, r *BackupResult) map[string]string {
	sums := map[string]string{}
	if len(files) == 0 {
		return sums
	}
	p.setPhase(PhaseBackup)
	if err := im.Backup.Ready(); err != nil {
		r.Failed = len(files)
		r.Errors = append(r.Errors, err.Error())
		p.BackupFailed.Add(int64(len(files)))
		p.BackupBytesC.Add(p.BackupBytes)
		im.logf("backup: %v", err)
		return sums
	}
	for i, f := range files {
		if ctx.Err() != nil {
			break
		}
		if im.Opt.DryRun {
			r.Copied++
			p.BackupDone.Add(1)
			p.BackupBytesC.Add(f.Size)
			continue
		}
		sum, err := im.Backup.Copy(ctx, f)
		p.BackupBytesC.Add(f.Size)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			r.Failed++
			p.BackupFailed.Add(1)
			if len(r.Errors) < 5 {
				r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", f.Name, err))
			}
			im.logf("backup of %s: %v", f.Path, err)
			if diskFull(err) {
				rest := len(files) - i - 1
				r.Failed += rest
				p.BackupFailed.Add(int64(rest))
				r.Errors = append(r.Errors, fmt.Sprintf("backup folder full, %d files not backed up", rest))
				break
			}
			continue
		}
		r.Copied++
		p.BackupDone.Add(1)
		if sum != "" {
			sums[f.Key()] = sum
		}
	}
	return sums
}

// upload checks and uploads the new files.
func (im *Importer) upload(ctx context.Context, p *Progress, fresh []File, sums map[string]string, res *Result) {

	var mu sync.Mutex // guards res
	fail := func(f File, err error) {
		p.Failed.Add(1)
		p.Done.Add(1)
		mu.Lock()
		res.Failed++
		if len(res.Errors) < 5 {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", f.Name, err))
		}
		mu.Unlock()
		im.logf("error on %s: %v", f.Path, err)
	}
	remember := func(f File) {
		if im.Opt.DryRun {
			return
		}
		if err := im.Mem.Add(f.Key()); err != nil {
			im.logf("write memory: %v", err)
		}
	}

	// 1. Checksums and duplicate check, in blocks: so the upload does not
	//    wait until the whole card has been read
	p.setPhase(PhaseChecking)
	jobs := make(chan job)
	var wg sync.WaitGroup
	for range max(im.Opt.Parallel, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				im.uploadOne(ctx, p, j, res, &mu, fail, remember)
			}
		}()
	}

	const block = 50
	for start := 0; start < len(fresh) && ctx.Err() == nil; start += block {
		chunk := fresh[start:min(start+block, len(fresh))]
		var items []job
		var check []immich.CheckItem
		for _, f := range chunk {
			sum, ok := sums[f.Key()]
			var err error
			if !ok {
				sum, err = sha1File(ctx, f.Path)
			}
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
					fail(j.f, fmt.Errorf("duplicate check: %w", err))
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
				im.logf("%s: rejected by Immich (%s)", j.f.Name, r.Reason)
				remember(j.f) // will never work, don't retry every time
			case im.Opt.DryRun:
				p.Created.Add(1) // dry run: "would upload"
				p.Done.Add(1)
				p.BytesUp.Add(j.f.Size)
				mu.Lock()
				res.Created++
				mu.Unlock()
			default:
				select {
				case jobs <- j:
					p.setPhase(PhaseUploading)
				case <-ctx.Done():
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
}

func (im *Importer) uploadOne(ctx context.Context, p *Progress, j job, res *Result, mu *sync.Mutex,
	fail func(File, error), remember func(File)) {
	var sent atomic.Int64
	r, err := im.Immich.Upload(ctx, j.f.Path, j.sum, func(n int64) {
		sent.Add(n)
		p.BytesUp.Add(n)
	})
	if err != nil {
		p.BytesUp.Add(j.f.Size - sent.Load()) // count as done for the progress
		if ctx.Err() != nil {
			return // cancelled: not an error, try again next time
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

// watchProgress reports progress at the percentages from PROGRESS_STEPS.
// If progress jumps past several of them, only the highest is reported.
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
				im.Notify(fmt.Sprintf("⏳ %d %% done (%s, %s), running for %s",
					hit, p.Card, p.Phase(), FmtDur(time.Since(p.Start))))
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
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}
