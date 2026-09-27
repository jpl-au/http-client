package client_test

import (
	"bytes"
	"compress/flate"
	"compress/lzw"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

func TestCompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	t.Run("standard", func(t *testing.T) {
		tests := []struct {
			name        string
			compression options.CompressionType
		}{
			{"gzip", options.CompressionGzip},
			{"deflate", options.CompressionDeflate},
			{"brotli", options.CompressionBrotli},
		}

		opt := options.New()

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				opt.SetCompression(tt.compression)

				t.Logf("[%s] Uncompressed size: %d bytes", tt.name, largefile.Len())

				resp, err := client.Post(server.URL+"/upload", largefile.String(), opt)
				if err != nil {
					t.Fatalf("Post() error = %v", err)
				}

				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if got := resp.String(); got != largefile.String() {
					t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), largefile.Len())
				}
			})
		}
	})

	t.Run("custom", func(t *testing.T) {
		tests := []struct {
			name        string
			compression options.CompressionType
			encoding    string
		}{
			{"flate", options.CompressionCustom, "flate"},
			{"lzw", options.CompressionCustom, "lzw"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				opt := options.New()
				opt.SetCompression(tt.compression)
				if tt.encoding == "flate" {
					opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
						return flate.NewWriter(w, flate.DefaultCompression)
					}
				}
				if tt.encoding == "lzw" {
					opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
						return lzw.NewWriter(w, lzw.LSB, 8), nil
					}
				}
				opt.Compression.CustomType = options.CompressionType(tt.encoding)
				t.Logf("Custom compression type set to: %s", opt.Compression.CustomType)

				t.Logf("[%s] Uncompressed size: %d bytes", tt.name, largefile.Len())

				resp, err := client.Post(server.URL+"/upload", largefile.String(), opt)
				if err != nil {
					t.Fatalf("Post() error = %v", err)
				}

				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if got := resp.String(); got != largefile.String() {
					t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), largefile.Len())
				}
			})
		}
	})
}

func TestDecompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	t.Run("standard", func(t *testing.T) {
		tests := []struct {
			name         string
			compression  string
			expectedSize int64
		}{
			{"gzip", "gzip", int64(largefile.Len())},
			{"deflate", "deflate", int64(largefile.Len())},
			{"brotli", "br", int64(largefile.Len())},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				opt := options.New()
				opt.SetBufferOutput()
				opt.EnableLogging()

				var bytesReceived int64
				opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
					bytesReceived = bytesRead // Just track total bytes read
				}

				url := fmt.Sprintf("%s/download/compressed?compression=%s", server.URL, tt.compression)
				resp, err := client.Get(url, opt)
				if err != nil {
					t.Fatalf("Request failed: %v", err)
				}

				// Debug info
				t.Logf("Response info: Status=%d, Len=%d, BodyEmpty=%v",
					resp.StatusCode, resp.Len(), resp.Body.IsEmpty())
				t.Logf("Response headers: %v", resp.Header)

				if resp.Body.IsEmpty() {
					t.Fatal("Response body is empty")
				}

				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if got := int64(resp.Len()); got != tt.expectedSize {
					t.Errorf("Len() = %d, want %d", got, tt.expectedSize)
				}
				if got := resp.Body.Bytes(); !bytes.Equal(got, largefile.Bytes()) {
					t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), largefile.Len())
				}
				if want := int64(largefile.Len()); bytesReceived != want {
					t.Errorf("bytes received = %d, want %d", bytesReceived, want)
				}

				t.Logf("[%s] Original size: %d, Compressed transfer",
					tt.name,
					largefile.Len())
			})
		}
	})

	t.Run("custom", func(t *testing.T) {
		tests := []struct {
			name        string
			compression options.CompressionType
			encoding    string
		}{
			{"flate", options.CompressionCustom, "flate"},
			{"lzw", options.CompressionCustom, "lzw"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				opt := options.New()
				opt.SetCompression(tt.compression)
				if tt.encoding == "flate" {
					opt.Compression.Decompressor = func(r io.Reader) (io.Reader, error) {
						return flate.NewReader(r), nil
					}
				}
				if tt.encoding == "lzw" {
					opt.Compression.Decompressor = func(r io.Reader) (io.Reader, error) {
						return lzw.NewReader(r, lzw.LSB, 8), nil
					}
				}
				opt.Compression.CustomType = options.CompressionType(tt.encoding)
				opt.SetBufferOutput()
				opt.EnableLogging()

				url := fmt.Sprintf("%s/download/compressed?compression=%s", server.URL, tt.encoding)
				resp, err := client.Get(url, opt)
				if err != nil {
					t.Fatalf("Request failed: %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if got := resp.Body.Bytes(); !bytes.Equal(got, largefile.Bytes()) {
					t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), largefile.Len())
				}
			})
		}
	})

	t.Run("to file", func(t *testing.T) {
		tests := []struct {
			name         string
			compression  string
			expectedSize int64
		}{
			{"gzip", "gzip", int64(largefile.Len())},
			{"deflate", "deflate", int64(largefile.Len())},
			{"brotli", "br", int64(largefile.Len())},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "download.txt")

				opt := options.New()
				opt.SetFileOutput(path)
				opt.EnableLogging()

				var bytesReceived int64
				opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
					bytesReceived = bytesRead
				}

				url := fmt.Sprintf("%s/download/compressed?compression=%s", server.URL, tt.compression)
				resp, err := client.Get(url, opt)
				if err != nil {
					t.Fatalf("Request failed: %v", err)
				}

				// Debug info
				t.Logf("Response info: Status=%d, Compression=%s", resp.StatusCode, tt.compression)
				t.Logf("Response headers: %v", resp.Header)

				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}

				// Read the downloaded file and verify its contents
				downloadedContent, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("ReadFile() error = %v", err)
				}
				if !bytes.Equal(downloadedContent, largefile.Bytes()) {
					t.Errorf("file content does not match: got %d bytes, want %d bytes", len(downloadedContent), largefile.Len())
				}

				// Verify file size matches expected size
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("Stat() error = %v", err)
				}
				if info.Size() != tt.expectedSize {
					t.Errorf("file size = %d, want %d", info.Size(), tt.expectedSize)
				}
				if want := int64(largefile.Len()); bytesReceived != want {
					t.Errorf("bytes received = %d, want %d", bytesReceived, want)
				}

				t.Logf("[%s] Original size: %d, File size: %d",
					tt.name,
					largefile.Len(),
					info.Size())
			})
		}
	})
}
