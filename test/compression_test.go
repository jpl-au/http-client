package client_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/golang/snappy"
	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/pierrec/lz4/v4"
	"github.com/stretchr/testify/assert"
)

func TestCompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name        string
		compression options.CompressionType
	}{
		{"Gzip Compression", options.CompressionGzip},
		{"Deflate Compression", options.CompressionDeflate},
		{"Brotli Compression", options.CompressionBrotli},
	}

	opt := options.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			opt.SetCompression(tt.compression)

			t.Logf("[%s] Uncompressed size: %d bytes", tt.name, largefile.Len())

			resp, err := client.Post(server.URL+"/upload", largefile.String(), opt)
			assert.NoError(t, err)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, largefile.String(), resp.String())
		})
	}
}

func TestCustomCompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name        string
		compression options.CompressionType
		encoding    string
	}{
		{"Snappy Compression", options.CompressionCustom, "snappy"},
		{"LZ4 Compression", options.CompressionCustom, "lz4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			opt := options.New()
			opt.SetCompression(tt.compression)
			if tt.encoding == "snappy" {
				opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
					return snappy.NewBufferedWriter(w), nil
				}
			}
			if tt.encoding == "lz4" {
				opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
					return lz4.NewWriter(w), nil
				}
			}
			opt.Compression.CustomType = options.CompressionType(tt.encoding)
			t.Logf("Custom compression type set to: %s", opt.Compression.CustomType)

			t.Logf("[%s] Uncompressed size: %d bytes", tt.name, largefile.Len())

			resp, err := client.Post(server.URL+"/upload", largefile.String(), opt)
			assert.NoError(t, err)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, largefile.String(), resp.String())
		})
	}
}

func TestStandardDecompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()
	tests := []struct {
		name         string
		compression  string
		expectedSize int64
	}{
		{"Gzip Decompression", "gzip", int64(largefile.Len())},
		{"Deflate Decompression", "deflate", int64(largefile.Len())},
		{"Brotli Decompression", "br", int64(largefile.Len())},
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

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tt.expectedSize, int64(resp.Len()))
			assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
			assert.Equal(t, int64(largefile.Len()), bytesReceived)

			t.Logf("[%s] Original size: %d, Compressed transfer",
				tt.name,
				largefile.Len())
		})
	}
}

func TestCustomDecompression(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()
	tests := []struct {
		name        string
		compression options.CompressionType
		encoding    string
	}{
		{"Snappy Decompression", options.CompressionCustom, "snappy"},
		{"LZ4 Decompression", options.CompressionCustom, "lz4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt.SetCompression(tt.compression)
			if tt.encoding == "snappy" {
				opt.Compression.Decompressor = func(r io.Reader) (io.Reader, error) {
					return snappy.NewReader(r), nil
				}
			}
			if tt.encoding == "lz4" {
				opt.Compression.Decompressor = func(r io.Reader) (io.Reader, error) {
					return lz4.NewReader(r), nil
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
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
		})
	}
}

func TestStandardDecompressionToFile(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name         string
		compression  string
		expectedSize int64
	}{
		{"Gzip Decompression", "gzip", int64(largefile.Len())},
		{"Deflate Decompression", "deflate", int64(largefile.Len())},
		{"Brotli Decompression", "br", int64(largefile.Len())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file for each test case
			tmpFile, err := os.CreateTemp("", fmt.Sprintf("decompress-%s-*.txt", tt.compression))
			assert.NoError(t, err)
			defer os.Remove(tmpFile.Name()) // Clean up after test
			tmpFile.Close()                 // Close it so the client can write to it

			opt := options.New()
			opt.SetFileOutput(tmpFile.Name())
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

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Read the downloaded file and verify its contents
			downloadedContent, err := os.ReadFile(tmpFile.Name())
			assert.NoError(t, err)
			assert.Equal(t, largefile.Bytes(), downloadedContent)

			// Verify file size matches expected size
			info, err := os.Stat(tmpFile.Name())
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedSize, info.Size())
			assert.Equal(t, int64(largefile.Len()), bytesReceived)

			t.Logf("[%s] Original size: %d, File size: %d",
				tt.name,
				largefile.Len(),
				info.Size())
		})
	}
}
