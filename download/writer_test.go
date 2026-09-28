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

// newFileWriter returns a writer that stages a download to path.
func newFileWriter(t *testing.T, path string) *fileWriter {
	t.Helper()
	w, err := newWriter(options.ResponseWriter{Type: options.WriteToFile, FilePath: path}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return w.(*fileWriter)
}

func TestFileWriter(t *testing.T) {
	// After Publish, Close does nothing and a second terminal operation
	// fails without changing the destination.
	t.Run("close after publish", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		w := newFileWriter(t, path)
		if _, err := io.WriteString(w, "new"); err != nil {
			t.Fatal(err)
		}
		if err := w.Publish(); err != nil {
			t.Fatal(err)
		}
		transfer := &standard{writer: w}
		for range 2 {
			if err := transfer.Close(); err != nil {
				t.Errorf("Close() after Publish error = %v", err)
			}
		}
		if err := w.Discard(); !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Discard() after Publish error = %v, want %v", err, fs.ErrClosed)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
			t.Errorf("destination = %q (error %v), want %q", got, err, "new")
		}
	})

	// Close cleans up once. A later Close returns the same cleanup error
	// instead of trying again.
	t.Run("close keeps cleanup error", func(t *testing.T) {
		w := newFileWriter(t, filepath.Join(t.TempDir(), "file"))
		// Removing the staging file first makes the cleanup fail.
		if err := os.Remove(w.Name()); err != nil {
			t.Fatal(err)
		}
		transfer := &standard{writer: w}
		first := transfer.Close()
		if !errors.Is(first, fs.ErrNotExist) {
			t.Fatalf("Close() error = %v, want %v", first, fs.ErrNotExist)
		}
		if second := w.Close(); second != first {
			t.Errorf("second Close() error = %v, want %v", second, first)
		}
		if err := w.Publish(); !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Publish() after Close error = %v, want %v", err, fs.ErrClosed)
		}
	})

	// A failed Publish removes the staging file. A later Close returns the
	// same cleanup error instead of trying again.
	t.Run("close after failed publish", func(t *testing.T) {
		w := newFileWriter(t, filepath.Join(t.TempDir(), "file"))
		// Closing the staging file first makes Publish fail.
		if err := w.File.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.Publish(); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("Publish() error = %v, want %v", err, fs.ErrClosed)
		}
		transfer := &standard{writer: w}
		first := transfer.Close()
		if !errors.Is(first, fs.ErrClosed) {
			t.Fatalf("Close() error = %v, want the cleanup error from Publish", first)
		}
		if second := w.Close(); second != first {
			t.Errorf("second Close() error = %v, want %v", second, first)
		}
		if err := w.Publish(); !errors.Is(err, fs.ErrClosed) {
			t.Errorf("second Publish() error = %v, want %v", err, fs.ErrClosed)
		}
		if _, err := os.Stat(w.Name()); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("staging file %s still exists (error %v)", w.Name(), err)
		}
	})
}

func TestPartialWriter(t *testing.T) {
	// Discard removes only the bytes this response added, and the partial
	// file keeps its earlier data.
	t.Run("discard rolls back response", func(t *testing.T) {
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
			t.Errorf("Close() after Discard error = %v", err)
		}
		if err := file.Publish(); !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Publish() after Discard error = %v, want %v", err, fs.ErrClosed)
		}
		if got, err := os.ReadFile(partial); err != nil || string(got) != "abc" {
			t.Errorf("partial file = %q (error %v), want %q", got, err, "abc")
		}
	})
}
