package importer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

// Backup copies card files to Dir/YYYY/YYYY-MM-DD/name, by the file's
// modification time. Mem remembers what is safely copied.
type Backup struct {
	Dir string
	Mem *memory.Store
}

// Ready reports why Dir cannot be used, nil if it can. Dir is never created:
// without a mapped folder it would land inside the docker image.
func (b *Backup) Ready() error {
	st, err := os.Stat(b.Dir)
	if err != nil {
		return fmt.Errorf("%s is not mapped – set the backup folder in the container template", b.Dir)
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", b.Dir)
	}
	f, err := os.CreateTemp(b.Dir, ".write-test-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", b.Dir, unwrapPath(err))
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// Copy copies f, verifies the copy against the card by checksum and returns
// the SHA1 (the import reuses it). An identical file that is already there
// (same name, size and time) counts as copied.
func (b *Backup) Copy(ctx context.Context, f File) (sum string, err error) {
	day := f.MTime.In(time.Local)
	dir := filepath.Join(b.Dir, day.Format("2006"), day.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", unwrapPath(err)
	}
	dest, exists := freeName(dir, f)
	if exists {
		return "", b.Mem.Add(f.Key())
	}

	tmp, err := os.CreateTemp(dir, "."+f.Name+".*.part")
	if err != nil {
		return "", unwrapPath(err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	src, err := os.Open(f.Path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	h := sha1.New()
	if _, err = copyCtx(ctx, tmp, io.TeeReader(src, h)); err != nil {
		return "", err
	}
	want := h.Sum(nil)
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	// Read the copy back: a backup that is meant to replace the card must
	// not be trusted unchecked
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h2 := sha1.New()
	if _, err = copyCtx(ctx, h2, tmp); err != nil {
		return "", err
	}
	if !bytes.Equal(want, h2.Sum(nil)) {
		return "", errors.New("copy differs from the card")
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Chtimes(tmp.Name(), f.MTime, f.MTime); err != nil {
		return "", err
	}
	if err = os.Rename(tmp.Name(), dest); err != nil {
		return "", err
	}
	return hex.EncodeToString(want), b.Mem.Add(f.Key())
}

// freeName returns the target path for f. exists is true if the same file
// (size and time) is already there. Another file with the same name, e.g.
// after the camera's counter wrapped, gets a suffix: DSC01234_1.ARW.
func freeName(dir string, f File) (path string, exists bool) {
	ext := filepath.Ext(f.Name)
	base := strings.TrimSuffix(f.Name, ext)
	for i := 0; ; i++ {
		name := f.Name
		if i > 0 {
			name = base + "_" + strconv.Itoa(i) + ext
		}
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			return p, false
		}
		if st.Size() == f.Size && st.ModTime().Unix() == f.MTime.Unix() {
			return p, true
		}
	}
}

func copyCtx(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 1<<20)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		r, err := src.Read(buf)
		if r > 0 {
			w, werr := dst.Write(buf[:r])
			n += int64(w)
			if werr != nil {
				return n, unwrapPath(werr)
			}
		}
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}

// diskFull: no point in trying the remaining files
func diskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }

func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
