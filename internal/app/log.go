package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Logger writes to stdout (Docker log) and to a file for /log.
// Above 1 MB the file is moved to .1.
type Logger struct {
	mu   sync.Mutex
	path string
	out  io.Writer
	max  int64
}

func NewLogger(path string, out io.Writer) *Logger {
	return &Logger{path: path, out: out, max: 1 << 20}
}

func (l *Logger) Printf(format string, args ...any) {
	line := time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, args...) + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	io.WriteString(l.out, line)
	if l.path == "" {
		return
	}
	if st, err := os.Stat(l.path); err == nil && st.Size() > l.max {
		os.Rename(l.path, l.path+".1")
	}
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		f.WriteString(line)
		f.Close()
	}
}

// Tail returns the last n lines, each at most width characters long.
func (l *Logger) Tail(n, width int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return "(empty so far)"
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if r := []rune(line); len(r) > width {
			line = string(r[:width]) + "…"
		}
		lines = append(lines, line)
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if len(lines) == 0 {
		return "(empty so far)"
	}
	return strings.Join(lines, "\n")
}
