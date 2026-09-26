//go:build unix

package client_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// fileMode returns the permission bits of path.
func fileMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	return info.Mode().Perm()
}

func TestFileOutputKeepsDestinationPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new report"))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(path, []byte("old report"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := client.Get(server.URL, options.New().SetFileOutput(path))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got, want := fileMode(t, path), fs.FileMode(0o600); got != want {
		t.Errorf("file mode after replace = %v, want %v: replacing a file must not widen its permissions", got, want)
	}
}

func TestFileOutputHonoursUmaskForNewFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("report"))
	}))
	defer server.Close()

	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)

	path := filepath.Join(t.TempDir(), "report.csv")
	_, err := client.Get(server.URL, options.New().SetFileOutput(path))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got, want := fileMode(t, path), fs.FileMode(0o600); got != want {
		t.Errorf("new file mode = %v, want %v: a new file must respect the process umask", got, want)
	}
}

func TestResumeKeepsDestinationPermissions(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "complete"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.csv")
			if err := os.WriteFile(path, []byte("old report"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			writePartial(t, path, "abc")
			if err := os.Chmod(options.PartialPath(path), 0o644); err != nil {
				t.Fatalf("Chmod() error = %v", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Range", "bytes 3-5/6")
				w.Header().Set("Content-Length", "3")
				w.WriteHeader(http.StatusPartialContent)
				if interrupted {
					_, _ = w.Write([]byte("d"))
				} else {
					_, _ = w.Write([]byte("def"))
				}
			}))
			defer server.Close()
			var checked bool
			opt := options.New().Resume(path, etag(`"v1"`)).OnDownloadProgress(func(_, _ int64) {
				checked = true
				if got, want := fileMode(t, options.PartialPath(path)), fs.FileMode(0o600); got != want {
					t.Errorf("partial file mode before write = %v, want %v: permissions must be corrected before writing", got, want)
				}
			})
			_, err := client.Get(server.URL, opt)
			if !checked {
				t.Fatal("OnDownloadProgress not called, want the response to report progress")
			}
			if interrupted {
				if err == nil {
					t.Fatal("Get() error = nil, want error")
				}
				if got, want := contentOrAbsent(t, path), "old report"; got != want {
					t.Errorf("destination content = %q, want %q", got, want)
				}
				if got, want := contentOrAbsent(t, options.PartialPath(path)), "abcd"; got != want {
					t.Errorf("partial content = %q, want %q", got, want)
				}
				if got, want := fileMode(t, options.PartialPath(path)), fs.FileMode(0o600); got != want {
					t.Errorf("partial file mode = %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				if got, want := contentOrAbsent(t, path), "abcdef"; got != want {
					t.Errorf("destination content = %q, want %q", got, want)
				}
			}
			if got, want := fileMode(t, path), fs.FileMode(0o600); got != want {
				t.Errorf("destination file mode = %v, want %v", got, want)
			}
		})
	}
}
