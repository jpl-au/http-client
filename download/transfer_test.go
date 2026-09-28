package download_test

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jpl-au/http-client/download"
	"github.com/jpl-au/http-client/options"
)

func TestNewKeepsTransferHeadersOutOfOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(options.PartialPath(path), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opt  *options.Option
		mode any
	}{
		{"resume", options.New().Resume(path, http.Header{"Etag": {`"v1"`}}), (*download.Resumable)(nil)},
		{"segmented", options.New().SetFileOutput(path).SetSegments(2), (*download.Segmented)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opt.AddHeader("X-Existing", "value")
			req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
			if err != nil {
				t.Fatal(err)
			}
			// prepareRequest historically shared this map with Option. New must
			// take ownership before it writes its generated headers.
			req.Header = tc.opt.Header
			transfer, err := download.New(req, &http.Client{}, tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := transfer.Close(); err != nil {
					t.Error(err)
				}
			})
			if req.Header.Get("Accept-Encoding") != "identity" || req.Header.Get("Range") == "" {
				t.Errorf("generated headers = %v", req.Header)
			}
			if req.Header.Get("X-Existing") != "value" {
				t.Errorf("existing header lost: %v", req.Header)
			}
			if tc.opt.Header.Get("Accept-Encoding") != "" || tc.opt.Header.Get("Range") != "" || tc.opt.Header.Get("If-Range") != "" {
				t.Errorf("options header mutated: %v", tc.opt.Header)
			}
			if tc.opt.Range.IsSet {
				t.Error("derived range changed configuration")
			}
			switch tc.mode.(type) {
			case *download.Resumable:
				if _, ok := transfer.(*download.Resumable); !ok {
					t.Errorf("New returned %T, want Resumable", transfer)
				}
			case *download.Segmented:
				if _, ok := transfer.(*download.Segmented); !ok {
					t.Errorf("New returned %T, want Segmented", transfer)
				}
			}
		})
	}
}

func TestNewReleasesClaimAfterSetupFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	partial := options.PartialPath(path)
	if err := os.Symlink(partial, partial); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	opt := options.New().Resume(path, nil)
	req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	if transfer, err := download.New(req, &http.Client{}, opt); err == nil || transfer != nil {
		t.Fatalf("New with unreadable partial file = (%T, %v), want (nil, error)", transfer, err)
	}
	if err := os.Remove(partial); err != nil {
		t.Fatal(err)
	}
	transfer, err := download.New(req, &http.Client{}, opt)
	if err != nil {
		t.Fatalf("New after setup failure: %v", err)
	}
	if err := transfer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewInitialisesNilRequestHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	opt := options.New().Resume(path, nil)
	req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = nil
	transfer, err := download.New(req, &http.Client{}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("Accept-Encoding = %q, want identity", got)
	}
	if err := transfer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsInvalidChecksumBeforeTransfer(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := download.New(req, &http.Client{}, options.New().SetChecksum(sha256.New, "invalid"))
	if err == nil || transfer != nil {
		t.Fatalf("New = (%T, %v), want (nil, invalid checksum error)", transfer, err)
	}
	if errors.Is(err, download.ErrChecksumMismatch) {
		t.Errorf("configuration error = %v, want validation rather than a data mismatch", err)
	}
}
