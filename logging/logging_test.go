package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterRotatesAndKeepsTheNewestArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// 10-byte limit, 3 archives: the smallest configuration that exercises every
	// boundary (first write, rotation, dropping the oldest).
	w, err := NewRotatingWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	// Six 8-byte lines: "line-0\n" … "line-5\n"
	for i := range 6 {
		line := []byte("line-" + string(rune('0'+i)) + "\n")
		if _, err := w.Write(line); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// The live file must never exceed the limit...
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat live file: %v", err)
	}
	if info.Size() > 10 {
		t.Errorf("live file is %d bytes, the limit was 10", info.Size())
	}

	// ...and no more than maxFiles archives may exist.
	archives, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(archives) > 3 {
		t.Errorf("%d archives exist, the limit was 3: %v", len(archives), archives)
	}

	// The most recent archive is .0 and holds the line written before the last
	// rotate: the newest data must be the one that survives.
	newest, err := os.ReadFile(path + ".0")
	if err != nil {
		t.Fatalf("reading .0: %v", err)
	}
	if !bytes.Contains(newest, []byte("line-4")) {
		t.Errorf("the newest archive should hold the most recent rotated line, got %q", newest)
	}

	// And the oldest archive must not hold the very first line anymore: with
	// only 3 archives, "line-0" has been dropped on purpose.
	for _, a := range archives {
		content, _ := os.ReadFile(a)
		if bytes.Contains(content, []byte("line-0")) {
			t.Errorf("%s still holds the oldest line: rotation is not discarding", a)
		}
	}
}

func TestRotatingWriterCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	w, err := NewRotatingWriter(filepath.Join(dir, "x.log"), 100, 2)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the log directory was not created: %v", err)
	}
}

func TestRotatingWriterRejectsBadArguments(t *testing.T) {
	if _, err := NewRotatingWriter("", 10, 2); err == nil {
		t.Error("an empty path should be an error")
	}
	if _, err := NewRotatingWriter(filepath.Join(t.TempDir(), "a.log"), 0, 2); err == nil {
		t.Error("maxBytes=0 should be an error (it would rotate on every write)")
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	w, err := NewRotatingWriter(filepath.Join(t.TempDir(), "a.log"), 100, 2)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := w.Write([]byte("hola\n")); err == nil {
		t.Error("writing after Close should fail")
	}
}

func TestNewWritesToFileAndStdout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, closer, err := New(Options{Path: path, Level: "debug", MaxBytes: 1 << 20, MaxFiles: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closer.Close()

	logger.Debug("mensaje de prueba", "clave", "valor")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log file: %v", err)
	}
	if !strings.Contains(string(content), "mensaje de prueba") || !strings.Contains(string(content), "clave=valor") {
		t.Errorf("the file log is missing the message: %q", content)
	}
}

func TestLevelFiltersBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, closer, err := New(Options{Path: path, Level: "info"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closer.Close()

	logger.Debug("no debería salir")
	logger.Info("sí debería salir")

	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "no debería") {
		t.Error("debug logs must be filtered out at level=info")
	}
	if !strings.Contains(string(content), "sí debería") {
		t.Error("info logs must be written at level=info")
	}
}

func TestParseLevelRejectsTypos(t *testing.T) {
	if _, err := ParseLevel("inf"); err == nil {
		t.Error("a typo in the level should be an error, not a silent info")
	}
	if lvl, err := ParseLevel("WARNING"); err != nil || lvl != slog.LevelWarn {
		t.Errorf("ParseLevel(WARNING) = %v, %v; want warn", lvl, err)
	}
}
