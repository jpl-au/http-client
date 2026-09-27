package client_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

func TestOption_SetFileOutput(t *testing.T) {
	t.Run("download with progress", func(t *testing.T) {
		server := setupTestServer(t)
		defer server.Close()

		downloadPath := filepath.Join(t.TempDir(), "download.txt")

		var lastProgress float64
		opt := options.New()
		opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
			if totalBytes > 0 {
				lastProgress = float64(bytesRead) / float64(totalBytes) * 100
			}
		}
		opt.SetFileOutput(downloadPath)

		resp, err := client.Get(server.URL+"/download", opt)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		if lastProgress != 100 {
			t.Errorf("last progress = %v, want %v", lastProgress, float64(100))
		}

		info, err := os.Stat(downloadPath)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if got, want := info.Size(), int64(largefile.Len()); got != want {
			t.Errorf("file size = %d, want %d", got, want)
		}
	})

	t.Run("bodyless responses", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code, _ := strconv.Atoi(r.URL.Query().Get("status"))
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(code)
		}))
		defer server.Close()

		tests := []struct {
			name   string
			method string
			status int
		}{
			{"head 200", http.MethodHead, http.StatusOK},
			{"get 204", http.MethodGet, http.StatusNoContent},
			{"get 304", http.MethodGet, http.StatusNotModified},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "existing.bin")
				writeFile(t, path, "existing")

				url := fmt.Sprintf("%s?status=%d", server.URL, tt.status)
				resp, err := client.Custom(tt.method, url, nil, options.New().SetFileOutput(path))
				if err != nil {
					t.Fatalf("Custom() error = %v", err)
				}

				if resp.StatusCode != tt.status {
					t.Errorf("StatusCode = %d, want %d (metadata kept)", resp.StatusCode, tt.status)
				}
				if got, want := resp.Header.Get("Content-Encoding"), "gzip"; got != want {
					t.Errorf("Header.Get(%q) = %q, want %q (metadata kept)", "Content-Encoding", got, want)
				}
				if got, want := readFile(t, path), "existing"; got != want {
					t.Errorf("file content = %q, want %q (a response without a body must not change the file)", got, want)
				}
			})
		}
	})

	t.Run("error status keeps destination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		defer server.Close()

		dir := t.TempDir()
		path := filepath.Join(dir, "download.bin")
		writeFile(t, path, "valid")

		resp, err := client.Get(server.URL, options.New().SetFileOutput(path))
		if err != nil {
			t.Fatalf("Get() error = %v, want nil (an error status is a response, not a transport failure)", err)
		}

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
		}
		if got, want := resp.String(), "server error\n"; got != want {
			t.Errorf("String() = %q, want %q (the error body in the response)", got, want)
		}
		if got, want := readFile(t, path), "valid"; got != want {
			t.Errorf("file content = %q, want %q (an error status must not change the file)", got, want)
		}
		if got, want := dirEntries(t, dir), []string{"download.bin"}; !slices.Equal(got, want) {
			t.Errorf("directory entries = %q, want %q", got, want)
		}
	})

	t.Run("truncated body keeps destination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\nbad"))
			conn.Close()
		}))
		defer server.Close()

		dir := t.TempDir()
		path := filepath.Join(dir, "download.bin")
		writeFile(t, path, "valid")

		_, err := client.Get(server.URL, options.New().SetFileOutput(path))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("Get() error = %v, want %v", err, io.ErrUnexpectedEOF)
		}

		if got, want := readFile(t, path), "valid"; got != want {
			t.Errorf("file content = %q, want %q (a truncated body must not replace the file)", got, want)
		}
		if got, want := dirEntries(t, dir), []string{"download.bin"}; !slices.Equal(got, want) {
			t.Errorf("directory entries = %q, want %q (the temporary file removed)", got, want)
		}
	})

	t.Run("success replaces destination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("new content"))
		}))
		defer server.Close()

		dir := t.TempDir()
		path := filepath.Join(dir, "download.bin")
		writeFile(t, path, "old")

		_, err := client.Get(server.URL, options.New().SetFileOutput(path))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}

		if got, want := readFile(t, path), "new content"; got != want {
			t.Errorf("file content = %q, want %q", got, want)
		}
		if got, want := dirEntries(t, dir), []string{"download.bin"}; !slices.Equal(got, want) {
			t.Errorf("directory entries = %q, want %q (the temporary file renamed into place)", got, want)
		}
	})

	// A download that asks for no range fails when the server answers with a
	// range, and keeps the destination, because the range is not the whole
	// file.
	t.Run("unrequested range rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-3/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("abcd"))
		}))
		defer server.Close()
		dir := t.TempDir()
		path := filepath.Join(dir, "download.bin")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		resp, err := client.Get(server.URL, options.New().SetFileOutput(path))
		if !errors.Is(err, client.ErrRangeMismatch) {
			t.Fatalf("Get() error = %v, want %v", err, client.ErrRangeMismatch)
		}
		if resp.StatusCode != http.StatusPartialContent {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusPartialContent)
		}
		if got := contentOrAbsent(t, path); got != "old" {
			t.Errorf("destination = %q, want %q", got, "old")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("directory holds %d entries, want only the destination", len(entries))
		}
	})
}

func TestBufferSizes(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name         string
		bufferSize   int
		expectedSize int64
	}{
		{"small buffer", 1024, int64(largefile.Len())},
		{"large buffer", 32768, int64(largefile.Len())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt.SetDownloadBufferSize(tt.bufferSize)

			start := time.Now()
			resp, err := client.Get(server.URL+"/download", opt)
			duration := time.Since(start)

			if err != nil {
				t.Errorf("Get() error = %v", err)
			}
			if got := resp.Len(); got != tt.expectedSize {
				t.Errorf("Len() = %d, want %d", got, tt.expectedSize)
			}

			t.Logf("Download with %d buffer took %v", tt.bufferSize, duration)
		})
	}
}

// writeFile creates path with content and fails the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the content of path and fails the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// dirEntries returns the names of the files in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
