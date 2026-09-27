package form_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jpl-au/http-client/form"
)

// writeFile writes content to name in a temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// received is one part of an encoded form.
type received struct {
	name, filename, contentType, content string
}

// encode reads the whole form and returns its bytes.
func encode(t *testing.T, f *form.Form) []byte {
	t.Helper()
	r := f.Reader()
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll(Reader()) error = %v", err)
	}
	return data
}

// parts parses the encoded form with the standard library.
func parts(t *testing.T, f *form.Form, data []byte) []received {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(f.ContentType())
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("ContentType() = %q, want multipart/form-data with a boundary", f.ContentType())
	}
	mr := multipart.NewReader(bytes.NewReader(data), params["boundary"])
	var got []received
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("NextPart() error = %v", err)
		}
		content, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("ReadAll(part) error = %v", err)
		}
		got = append(got, received{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), string(content)})
	}
}

func TestPartsKeepTheirOrder(t *testing.T) {
	first := writeFile(t, "first.txt", "one")
	second := writeFile(t, "second.json", `{"n":2}`)
	f := form.New().
		Field("title", "Report").
		File("attachment", first).
		Field("tag", "a").
		File("attachment", second).
		Field("tag", "b")

	got := parts(t, f, encode(t, f))
	want := []received{
		{"title", "", "", "Report"},
		{"attachment", "first.txt", "text/plain; charset=utf-8", "one"},
		{"tag", "", "", "a"},
		{"attachment", "second.json", "application/json", `{"n":2}`},
		{"tag", "", "", "b"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("parts = %q, want %q", got, want)
	}
}

func TestLenMatchesEncodedLength(t *testing.T) {
	f := form.New().
		Field("name", `a "quoted" value`).
		File("upload", writeFile(t, "data.bin", "0123456789")).
		File("empty", writeFile(t, "empty.txt", ""))

	n, err := f.Len()
	if err != nil {
		t.Fatalf("Len() error = %v", err)
	}
	if got := int64(len(encode(t, f))); got != n {
		t.Errorf("encoded length = %d, Len() = %d", got, n)
	}
}

func TestReaderCanBeReadAgain(t *testing.T) {
	f := form.New().Field("k", "v").File("upload", writeFile(t, "data.txt", "content"))

	first, second := encode(t, f), encode(t, f)
	if string(first) != string(second) {
		t.Error("second encoding differs from the first")
	}
}

func TestLenReportsMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	_, err := form.New().File("upload", missing).Len()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Len() error = %v, want %v", err, fs.ErrNotExist)
	}
}

func TestReaderReportsChangedFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"longer", "0123456789 and more"},
		{"shorter", "01234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, "data.txt", "0123456789")
			f := form.New().File("upload", path)
			if _, err := f.Len(); err != nil {
				t.Fatalf("Len() error = %v", err)
			}

			r := f.Reader()
			defer r.Close()
			// Read part of the headers, which the reader sends before it copies
			// the file, and change the file while the reader waits.
			buf := make([]byte, 16)
			if _, err := io.ReadFull(r, buf); err != nil {
				t.Fatalf("ReadFull() error = %v", err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := io.ReadAll(r); !errors.Is(err, form.ErrFileChanged) {
				t.Errorf("ReadAll() error = %v, want %v", err, form.ErrFileChanged)
			}
		})
	}
}

func TestCloseStopsTheReader(t *testing.T) {
	f := form.New().File("upload", writeFile(t, "data.txt", "content"))

	r := f.Reader()
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Error("Read() after Close() error = nil, want error")
	}
}
