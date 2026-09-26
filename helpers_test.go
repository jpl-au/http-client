package client_test

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/lzw"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// testPattern is the repeated text of the fixtures. Unlike random data, it compresses well.
const testPattern = "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. "

// The fixtures are the contents and paths of a 1 MiB and a 10 MiB file of testPattern.
var (
	smallfile *bytes.Buffer
	largefile *bytes.Buffer
	smallf    string
	largef    string
)

// TestMain writes the fixtures to a temporary directory and removes it after the tests run.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "http-client-test-")
	if err != nil {
		log.Fatalf("create fixture directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("remove fixture directory: %v", err)
		}
	}()

	smallfile = bytes.NewBufferString(generateTestData(1 << 20))
	largefile = bytes.NewBufferString(generateTestData(10 << 20))
	smallf = filepath.Join(dir, "test-small.txt")
	largef = filepath.Join(dir, "test-large.txt")
	for path, data := range map[string]*bytes.Buffer{smallf: smallfile, largef: largefile} {
		if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
			log.Fatalf("write fixture: %v", errors.Join(err, os.RemoveAll(dir)))
		}
	}

	m.Run()
}

func generateTestData(length int) string {
	result := make([]byte, length)
	patternLen := len(testPattern)
	for i := range result {
		result[i] = testPattern[i%patternLen]
	}
	return string(result)
}

func setupTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			// Handle decompression based on Content-Encoding
			var err error
			var reader io.Reader
			var buff bytes.Buffer

			if r.Header.Get("Content-Encoding") != "" || r.Header.Get("X-DATA") != "" {
				t.Logf("Content-Encoding: %s", r.Header.Get("Content-Encoding"))
				t.Logf("Content-Length: %s", r.Header.Get("Content-Length"))
			}
			// Decompress based on Content-Encoding and read into buffer
			switch r.Header.Get("Content-Encoding") {
			case "gzip":
				t.Log("Using gzip reader")
				gzipReader, err := gzip.NewReader(r.Body)
				if err != nil {
					log.Printf("Failed to create gzip reader: %v", err)
					http.Error(w, "Failed to create gzip reader: "+err.Error(), http.StatusBadRequest)
					return
				}
				defer gzipReader.Close()
				reader = gzipReader
			case "deflate":
				t.Log("Using deflate reader")
				zlibReader, err := zlib.NewReader(r.Body)
				if err != nil {
					log.Printf("Failed to create gzip reader: %v", err)
					http.Error(w, "Failed to create gzip reader: "+err.Error(), http.StatusBadRequest)
					return
				}
				defer zlibReader.Close()
				reader = zlibReader
			case "br":
				t.Log("Using Brotli reader")
				reader = brotli.NewReader(r.Body)
			case "flate":
				t.Log("Using flate reader")
				reader = flate.NewReader(r.Body)
			case "lzw":
				t.Log("Using LZW reader")
				reader = lzw.NewReader(r.Body, lzw.LSB, 8)
			default:
				reader = r.Body
			}

			// Read the data into the buffer - decompressing it if necessary
			_, err = io.Copy(&buff, reader)
			if err != nil {
				t.Logf("reader err: %s", err)
				http.Error(w, "Failed to copy data to the buffer:"+err.Error(), http.StatusInternalServerError)
				return
			}

			if r.Header.Get("Content-Encoding") != "" || r.Header.Get("X-DATA") != "" {
				// Once decompressed, send the data back to the client
				t.Logf("Server file size: %d bytes", buff.Len())
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				http.Error(w, "Failed to send decompressed data:"+err.Error(), http.StatusInternalServerError)
				return
			}

		case "/upload/redirect":
			t.Logf("redirecting to /upload")
			http.Redirect(w, r, "/upload", http.StatusTemporaryRedirect)

		case "/upload/no-preserve":
			t.Logf("redirecting to /method-check")
			http.Redirect(w, r, "/method-check", http.StatusFound)

		case "/method-check":
			_, _ = w.Write([]byte(r.Method))

		case "/max-redirects":
			t.Logf("redirecting to /max-redirects")
			http.Redirect(w, r, "/max-redirects", http.StatusFound)

		case "/upload/multipart":
			err := r.ParseMultipartForm(200 << 20) // 200 MB max memory
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			fileInfo := make(map[string]int64)

			for _, files := range r.MultipartForm.File {
				for _, fileHeader := range files {
					file, err := fileHeader.Open()
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					defer file.Close()

					// Get file size
					size, err := file.Seek(0, io.SeekEnd)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					_, _ = file.Seek(0, io.SeekStart) // Reset file pointer

					fileInfo[fileHeader.Filename] = size
				}
			}

			// Set content type to JSON
			w.Header().Set("Content-Type", "application/json")

			// Encode and write the JSON response
			if err := json.NewEncoder(w).Encode(fileInfo); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		case "/download":
			w.Header().Set("Content-Length", strconv.FormatInt(int64(largefile.Len()), 10)) // size of the large file
			_, _ = w.Write(largefile.Bytes())

		case "/download/range":
			// Endpoint that supports Range requests (RFC 7233)
			data := largefile.Bytes()
			totalSize := int64(len(data))

			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("ETag", `"large"`)

			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				// No range requested - return full content
				w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}

			// Parse Range header: "bytes=start-end" or "bytes=start-" or "bytes=-suffix"
			if !strings.HasPrefix(rangeHeader, "bytes=") {
				http.Error(w, "Invalid range unit", http.StatusBadRequest)
				return
			}

			rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
			var start, end int64

			if strings.HasPrefix(rangeSpec, "-") {
				// Suffix range: "-N" means last N bytes
				suffix, err := strconv.ParseInt(rangeSpec[1:], 10, 64)
				if err != nil || suffix <= 0 {
					http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
					return
				}
				start = max(totalSize-suffix, 0)
				end = totalSize - 1
			} else if strings.HasSuffix(rangeSpec, "-") {
				// Open-ended range: "N-" means from N to end
				var err error
				start, err = strconv.ParseInt(strings.TrimSuffix(rangeSpec, "-"), 10, 64)
				if err != nil || start < 0 || start >= totalSize {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
					http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
					return
				}
				end = totalSize - 1
			} else {
				// Explicit range: "N-M"
				parts := strings.Split(rangeSpec, "-")
				if len(parts) != 2 {
					http.Error(w, "Invalid range format", http.StatusBadRequest)
					return
				}
				var err error
				start, err = strconv.ParseInt(parts[0], 10, 64)
				if err != nil {
					http.Error(w, "Invalid range start", http.StatusBadRequest)
					return
				}
				end, err = strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					http.Error(w, "Invalid range end", http.StatusBadRequest)
					return
				}
			}

			// Validate range
			if start < 0 || end >= totalSize || start > end {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}

			// Return partial content
			contentLength := end - start + 1
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
			w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : end+1])

		case "/download/no-range":
			// Endpoint that doesn't support Range requests (ignores Range header, returns 200)
			w.Header().Set("Accept-Ranges", "none")
			w.Header().Set("Content-Length", strconv.FormatInt(int64(largefile.Len()), 10))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(largefile.Bytes())

		case "/download/compressed":
			compression := r.URL.Query().Get("compression")
			w.Header().Set("Content-Encoding", compression)

			var writer io.WriteCloser
			switch compression {
			case "gzip":
				writer = gzip.NewWriter(w)
			case "deflate":
				writer = zlib.NewWriter(w)
			case "br":
				writer = brotli.NewWriter(w)
			case "flate":
				var err error
				writer, err = flate.NewWriter(w, flate.DefaultCompression)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			case "lzw":
				writer = lzw.NewWriter(w, lzw.LSB, 8)
			default:
				http.Error(w, "unsupported compression", http.StatusBadRequest)
				return
			}
			defer writer.Close()

			_, err := io.Copy(writer, bytes.NewReader(largefile.Bytes()))
			if err != nil {
				t.Logf("Compression error: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		case "/echo-headers":
			// Echo back the received headers
			for name, values := range r.Header {
				w.Header().Set("Echo-"+name, strings.Join(values, ", "))
			}

		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Hello from path: %s", r.URL.Path)
		}
	}))
}
