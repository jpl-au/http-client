package client_test

import (
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/download"
	"github.com/jpl-au/http-client/options"
)

type closeTrackingReader struct {
	*strings.Reader
	closed bool
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestDownloadSetupClosesOpenedRequestBody(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer server.Close()
	body := &closeTrackingReader{Reader: strings.NewReader("payload")}
	resp, err := client.Post(server.URL, body, options.New().SetChecksum(sha256.New, "invalid"))
	if err == nil || !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("Post error = %v, want invalid checksum", err)
	}
	if !body.closed {
		t.Error("opened request body was not closed after download setup failed")
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("server received %d requests, want none", got)
	}
	if resp.Error == nil {
		t.Error("response did not record setup failure")
	}
}

func TestDownloadErrorAliases(t *testing.T) {
	for _, tc := range []struct {
		root, download error
	}{
		{client.ErrRangeMismatch, download.ErrRangeMismatch},
		{client.ErrDownloadIncomplete, download.ErrIncomplete},
		{client.ErrDownloadInProgress, download.ErrInProgress},
		{client.ErrChecksumMismatch, download.ErrChecksumMismatch},
		{client.ErrBodyTooLarge, download.ErrBodyTooLarge},
	} {
		if tc.root != tc.download || !errors.Is(tc.root, tc.download) {
			t.Errorf("root error %v and download error %v differ", tc.root, tc.download)
		}
	}
}
