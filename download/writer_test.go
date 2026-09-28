package download

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/jpl-au/http-client/options"
)

func TestFileWriterTerminalOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(options.ResponseWriter{Type: options.WriteToFile, FilePath: path}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "new"); err != nil {
		t.Fatal(err)
	}
	file := w.(*fileWriter)
	if err := file.Publish(); err != nil {
		t.Fatal(err)
	}
	transfer := &standard{writer: file}
	for range 2 {
		if err := transfer.Close(); err != nil {
			t.Errorf("transfer Close after Publish: %v", err)
		}
	}
	if err := file.Discard(); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("Discard after Publish = %v, want ErrClosed", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
		t.Errorf("destination = %q, %v; want new", got, err)
	}
}

func TestPartialWriterRollbackAndRepeatedClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	partial := options.PartialPath(path)
	if err := os.WriteFile(partial, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(options.ResponseWriter{Type: options.WriteToFile, FilePath: path}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "def"); err != nil {
		t.Fatal(err)
	}
	file := w.(*partialWriter)
	if err := file.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Errorf("Close after Discard: %v", err)
	}
	if err := file.Publish(); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("Publish after Discard = %v, want ErrClosed", err)
	}
	if got, err := os.ReadFile(partial); err != nil || string(got) != "abc" {
		t.Errorf("partial = %q, %v; want abc", got, err)
	}
}

func TestWriterCloseRetainsCleanupError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	w, err := newWriter(options.ResponseWriter{Type: options.WriteToFile, FilePath: path}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	file := w.(*fileWriter)
	if err := file.File.Close(); err != nil {
		t.Fatal(err)
	}
	first := file.Publish()
	if !errors.Is(first, fs.ErrClosed) {
		t.Fatalf("Publish after underlying close = %v, want ErrClosed", first)
	}
	transfer := &standard{writer: file}
	for range 2 {
		if err := transfer.Close(); !errors.Is(err, fs.ErrClosed) {
			t.Errorf("repeated transfer Close = %v, want retained cleanup error", err)
		}
	}
	if err := file.Publish(); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("repeated Publish = %v, want ErrClosed", err)
	}
	if _, err := os.Stat(file.Name()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("staging file still exists: %v", err)
	}
}
