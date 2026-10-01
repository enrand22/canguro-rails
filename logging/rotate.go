package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// RotatingWriter is an io.WriteCloser that rotates a log file by size, keeping a
// fixed number of archives: app.log, app.log.0 … app.log.N-1, where .0 is the
// most recent archive (the convention Ruby's Logger and logrotate both use).
//
// Why not just let the process write forever: a daemon that runs for months
// fills a disk, and a full disk takes down more than the app. Rotation belongs
// to the process that owns the file.
type RotatingWriter struct {
	path     string
	maxBytes int64
	maxFiles int

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewRotatingWriter opens path (creating it if needed) and rotates it once it
// exceeds maxBytes, keeping at most maxFiles archives.
func NewRotatingWriter(path string, maxBytes int64, maxFiles int) (*RotatingWriter, error) {
	if path == "" {
		return nil, fmt.Errorf("logging: empty log path")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("logging: maxBytes must be positive (got %d)", maxBytes)
	}
	if maxFiles < 1 {
		maxFiles = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("logging: creating log directory: %w", err)
	}

	w := &RotatingWriter{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("logging: opening %s: %w", w.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("logging: stating %s: %w", w.path, err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// Write implements io.Writer. It rotates first when the incoming write would
// cross the size limit, so no single line can be split across two files.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, fmt.Errorf("logging: writer is closed")
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("logging: writing to %s: %w", w.path, err)
	}
	return n, nil
}

// rotate archives the current file and starts a new one. Must be called with
// the mutex held.
func (w *RotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("logging: closing before rotate: %w", err)
	}

	// Drop the oldest archive, then shift every archive one position up...
	oldest := w.archiveName(w.maxFiles - 1)
	_ = os.Remove(oldest)
	for i := w.maxFiles - 2; i >= 0; i-- {
		from := w.archiveName(i)
		if _, err := os.Stat(from); err != nil {
			continue // doesn't exist yet
		}
		to := w.archiveName(i + 1)
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("logging: rotating %s -> %s: %w", from, to, err)
		}
	}
	// ...and the live file becomes .0.
	if err := os.Rename(w.path, w.archiveName(0)); err != nil {
		return fmt.Errorf("logging: archiving %s: %w", w.path, err)
	}
	return w.open()
}

func (w *RotatingWriter) archiveName(i int) string {
	return w.path + "." + strconv.Itoa(i)
}

// Close closes the underlying file. Writes after Close fail.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// ArchiveNames returns the existing archives, newest first. Useful in tests and
// in a `make logs` target.
func (w *RotatingWriter) ArchiveNames() []string {
	matches, _ := filepath.Glob(w.path + ".*")
	sort.Slice(matches, func(i, j int) bool {
		return archiveIndex(matches[i]) < archiveIndex(matches[j])
	})
	return matches
}

// Tee writes to several writers at once, keeping the first error. It is how the
// logger reaches the file AND stdout (so journald keeps working).
func Tee(writers ...io.Writer) io.Writer {
	return io.MultiWriter(writers...)
}

func archiveIndex(path string) int {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return 1 << 30
	}
	n, err := strconv.Atoi(path[i+1:])
	if err != nil {
		return 1 << 30
	}
	return n
}
