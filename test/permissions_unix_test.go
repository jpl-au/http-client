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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileMode returns the permission bits of path.
func fileMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

func TestFileOutputKeepsDestinationPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new report"))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "report.csv")
	require.NoError(t, os.WriteFile(path, []byte("old report"), 0o600))
	require.NoError(t, os.Chmod(path, 0o600))

	_, err := client.Get(server.URL, options.New().SetFileOutput(path))
	require.NoError(t, err)

	assert.Equal(t, fs.FileMode(0o600), fileMode(t, path), "replacing a file must not widen its permissions")
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
	require.NoError(t, err)

	assert.Equal(t, fs.FileMode(0o600), fileMode(t, path), "a new file must respect the process umask")
}

func TestResumeKeepsDestinationPermissions(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "complete"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.csv")
			require.NoError(t, os.WriteFile(path, []byte("old report"), 0o600))
			writePartial(t, path, "abc")
			require.NoError(t, os.Chmod(options.PartialPath(path), 0o644))
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
				assert.Equal(t, fs.FileMode(0o600), fileMode(t, options.PartialPath(path)), "permissions must be corrected before writing")
			})
			_, err := client.Get(server.URL, opt)
			require.True(t, checked, "the response should report progress")
			if interrupted {
				require.Error(t, err)
				assert.Equal(t, "old report", contentOrAbsent(t, path))
				assert.Equal(t, "abcd", contentOrAbsent(t, options.PartialPath(path)))
				assert.Equal(t, fs.FileMode(0o600), fileMode(t, options.PartialPath(path)))
			} else {
				require.NoError(t, err)
				assert.Equal(t, "abcdef", contentOrAbsent(t, path))
			}
			assert.Equal(t, fs.FileMode(0o600), fileMode(t, path))
		})
	}
}
