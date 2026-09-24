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
