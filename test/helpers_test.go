package client_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
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
	"time"

	"github.com/andybalholm/brotli"
	"github.com/golang/snappy"
	"github.com/pierrec/lz4/v4"
	"gopkg.in/yaml.v3"
)

const (
	chunkSize = 1024 * 1024 // 1MB chunks for writing
	smallf    = "test-small.txt"
	largef    = "test-large.txt"
	downloadf = "test-download.txt"

	// Repeated text pattern for test files - compresses well unlike random data
	testPattern = "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. "
)

var (
	smallfile         *bytes.Buffer
	largefile         *bytes.Buffer
	globalTestResults []TestResultSet
)

func init() {
	// create files for testing
	// it is just easier to clean them up manually after tests
	// are done vs. re-generating them each time
	createTestFile(smallf, 1)
	// load small file in to memory
	buf, err := os.ReadFile(smallf)
	if err != nil {
		log.Fatal("error loading small.txt: %w", err)
	}
	smallfile = bytes.NewBuffer(buf)

	createTestFile(largef, 10)
	// load large file in to memory
	buf, err = os.ReadFile(largef)
	if err != nil {
		log.Fatal("error loading large.txt: %w", err)
	}
	largefile = bytes.NewBuffer(buf)
}

func createTestFile(filename string, size int) {
	filesize := size * 1024 * 1024

	// Check if the file exists
	if fileInfo, err := os.Stat(filename); err == nil {
		// File exists, check its size
		if fileInfo.Size() == int64(filesize) {
			log.Printf("File %s already exists and is the correct size (%d bytes).", filename, filesize)
			return
		}
		log.Printf("File %s exists but is the wrong size (%d bytes). Recreating.", filename, fileInfo.Size())
	} else if !os.IsNotExist(err) {
		log.Fatalf("Error checking file %s: %v", filename, err)
	}

	// Create the file
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	bytesWritten := 0

	for bytesWritten < filesize {
		chunk := generateTestData(chunkSize)
		n, err := writer.WriteString(chunk)
		if err != nil {
			log.Fatal(err)
		}
		bytesWritten += n
	}

	err = writer.Flush()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("File %s created successfully with size %d bytes.", filename, filesize)
}

func generateTestData(length int) string {
	result := make([]byte, length)
	patternLen := len(testPattern)
	for i := range result {
		result[i] = testPattern[i%patternLen]
	}
	return string(result)
}

// TestResultSet represents a complete set of test results with metadata
type TestResultSet struct {
	Timestamp   time.Time    `yaml:"timestamp"`
	TestName    string       `yaml:"test_name"`
	Environment string       `yaml:"environment"`
	Results     []TestResult `yaml:"results"`
}

// TestResult represents a single test scenario result
type TestResult struct {
	ScenarioName   string        `yaml:"scenario_name"`
	NumGoroutines  int           `yaml:"num_goroutines"`
	RequestsPerGo  int           `yaml:"requests_per_go"`
	TotalRequests  int           `yaml:"total_requests"`
	Duration       time.Duration `yaml:"duration"`
	RequestsPerSec float64       `yaml:"requests_per_sec"`
	SuccessRate    float64       `yaml:"success_rate"`
	ErrorCount     int           `yaml:"error_count"`
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
			case "snappy":
				t.Log("Using Snappy reader")
				reader = snappy.NewReader(r.Body)
			case "lz4":
				t.Log("Using LZ4 reader")
				reader = lz4.NewReader(r.Body)
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
			http.Redirect(w, r, "/upload", http.StatusFound)

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
				start = totalSize - suffix
				if start < 0 {
					start = 0
				}
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
			case "snappy":
				writer = snappy.NewBufferedWriter(w)
			case "lz4":
				writer = lz4.NewWriter(w)
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

func writeResultsToFile(resultSet TestResultSet) error {
	dir := "test_results"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	filename := filepath.Join(dir, "performance_results.yaml")

	// Open file in append mode
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := yaml.Marshal(resultSet)
	if err != nil {
		return err
	}

	// Write document separator and data
	if _, err := f.WriteString("---\n"); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}

	return f.Sync() // Ensure data is written to disk
}
