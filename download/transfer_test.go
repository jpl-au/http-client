package download_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jpl-au/http-client/download"
	"github.com/jpl-au/http-client/options"
)

// newRequest returns a GET request for a file.
func newRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestNew(t *testing.T) {
	// The headers a transfer adds belong to the request, not to the options,
	// which the caller can use again.
	t.Run("headers stay out of options", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(options.PartialPath(path), []byte("abc"), 0o600); err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name string
			opt  *options.Option
			want string
		}{
			{"resume", options.New().Resume(path, http.Header{"Etag": {`"v1"`}}), "*download.Resumable"},
			{"segmented", options.New().SetFileOutput(path).SetSegments(2), "*download.Segmented"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.opt.AddHeader("X-Existing", "value")
				req := newRequest(t)
				// The request can share its header map with the options, so
				// New must copy it before it adds headers.
				req.Header = tt.opt.Header
				transfer, err := download.New(req, &http.Client{}, tt.opt)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := transfer.Close(); err != nil {
						t.Error(err)
					}
				})
				if got := fmt.Sprintf("%T", transfer); got != tt.want {
					t.Errorf("New() returned %s, want %s", got, tt.want)
				}
				if req.Header.Get("Accept-Encoding") != "identity" || req.Header.Get("Range") == "" {
					t.Errorf("request headers = %v, want Accept-Encoding and Range", req.Header)
				}
				if req.Header.Get("X-Existing") != "value" {
					t.Errorf("request headers = %v, want X-Existing kept", req.Header)
				}
				if tt.opt.Header.Get("Accept-Encoding") != "" || tt.opt.Header.Get("Range") != "" || tt.opt.Header.Get("If-Range") != "" {
					t.Errorf("options headers = %v, want no transfer headers", tt.opt.Header)
				}
				if tt.opt.Range.IsSet {
					t.Error("Range.IsSet = true, want the options range unchanged")
				}
			})
		}
	})

	// A failed setup releases the partial file, so the next download of the
	// same file can claim it.
	t.Run("setup failure releases claim", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		partial := options.PartialPath(path)
		// A symbolic link to itself cannot be read, so setup fails.
		if err := os.Symlink(partial, partial); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		opt := options.New().Resume(path, nil)
		req := newRequest(t)

		if transfer, err := download.New(req, &http.Client{}, opt); err == nil || transfer != nil {
			t.Fatalf("New() = (%T, %v), want (nil, error)", transfer, err)
		}
		if err := os.Remove(partial); err != nil {
			t.Fatal(err)
		}
		transfer, err := download.New(req, &http.Client{}, opt)
		if err != nil {
			t.Fatalf("New() after setup failure error = %v", err)
		}
		if err := transfer.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil request headers", func(t *testing.T) {
		opt := options.New().Resume(filepath.Join(t.TempDir(), "file"), nil)
		req := newRequest(t)
		req.Header = nil

		transfer, err := download.New(req, &http.Client{}, opt)
		if err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want %q", got, "identity")
		}
		if err := transfer.Close(); err != nil {
			t.Fatal(err)
		}
	})

	// An invalid checksum is a configuration error, not a mismatch in the
	// data.
	t.Run("invalid checksum", func(t *testing.T) {
		transfer, err := download.New(newRequest(t), &http.Client{}, options.New().SetChecksum(sha256.New, "invalid"))
		if err == nil || transfer != nil {
			t.Fatalf("New() = (%T, %v), want (nil, error)", transfer, err)
		}
		if errors.Is(err, download.ErrChecksumMismatch) {
			t.Errorf("New() error = %v, want an error that is not %v", err, download.ErrChecksumMismatch)
		}
	})
}
