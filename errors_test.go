package client_test

import (
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/download"
)

// The download errors are defined in the download package. The client
// package names the same values, so errors.Is matches either name.
func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		root     error
		download error
	}{
		{"ErrRangeMismatch", client.ErrRangeMismatch, download.ErrRangeMismatch},
		{"ErrDownloadIncomplete", client.ErrDownloadIncomplete, download.ErrIncomplete},
		{"ErrDownloadInProgress", client.ErrDownloadInProgress, download.ErrInProgress},
		{"ErrChecksumMismatch", client.ErrChecksumMismatch, download.ErrChecksumMismatch},
		{"ErrBodyTooLarge", client.ErrBodyTooLarge, download.ErrBodyTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.root != tt.download {
				t.Errorf("client.%s = %v, want the download package's value %v", tt.name, tt.root, tt.download)
			}
		})
	}
}
