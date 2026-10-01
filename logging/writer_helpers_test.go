package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveNamesReturnsThemNewestFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	w, err := NewRotatingWriter(path, 8, 3)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	for i := range 4 {
		if _, err := w.Write([]byte("line-" + string(rune('0'+i)) + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	names := w.ArchiveNames()
	if len(names) == 0 {
		t.Fatal("after rotating there should be archives")
	}
	if names[0] != path+".0" {
		t.Errorf("first archive = %s, want %s.0 (newest first)", names[0], path)
	}
	for _, n := range names {
		if _, err := os.Stat(n); err != nil {
			t.Errorf("ArchiveNames returned a file that does not exist: %s", n)
		}
	}
}

func TestTeeWritesEverywhere(t *testing.T) {
	var a, b bytes.Buffer
	w := Tee(&a, &b)
	if _, err := w.Write([]byte("hola")); err != nil {
		t.Fatalf("Tee write: %v", err)
	}
	if a.String() != "hola" || b.String() != "hola" {
		t.Errorf("Tee should write to both writers, got %q and %q", a.String(), b.String())
	}
}
