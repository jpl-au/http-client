package client_test

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

func TestPartialContentResponse(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	tests := []struct {
		name          string
		setup         func(*options.Option) *options.Option
		wantStatus    int
		wantPartial   bool
		checkResponse func(t *testing.T, bodyLen int)
	}{
		{
			name: "explicit range returns 206",
			setup: func(opt *options.Option) *options.Option {
				return opt.SetRange(0, 999)
			},
			wantStatus:  206,
			wantPartial: true,
			checkResponse: func(t *testing.T, bodyLen int) {
				if bodyLen != 1000 {
					t.Errorf("expected 1000 bytes, got %d", bodyLen)
				}
			},
		},
		{
			name: "range from offset returns 206",
			setup: func(opt *options.Option) *options.Option {
				return opt.SetRangeFrom(int64(largefile.Len() - 500))
			},
			wantStatus:  206,
			wantPartial: true,
			checkResponse: func(t *testing.T, bodyLen int) {
				if bodyLen != 500 {
					t.Errorf("expected 500 bytes, got %d", bodyLen)
				}
			},
		},
		{
			name: "last N bytes returns 206",
			setup: func(opt *options.Option) *options.Option {
				return opt.SetRangeLast(256)
			},
			wantStatus:  206,
			wantPartial: true,
			checkResponse: func(t *testing.T, bodyLen int) {
				if bodyLen != 256 {
					t.Errorf("expected 256 bytes, got %d", bodyLen)
				}
			},
		},
		{
			name: "no range returns 200 with full content",
			setup: func(opt *options.Option) *options.Option {
				return opt
			},
			wantStatus:  200,
			wantPartial: false,
			checkResponse: func(t *testing.T, bodyLen int) {
				if bodyLen != largefile.Len() {
					t.Errorf("expected %d bytes, got %d", largefile.Len(), bodyLen)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt = tt.setup(opt)

			resp, err := c.Get(server.URL+"/download/range", opt)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if resp.IsPartialContent != tt.wantPartial {
				t.Errorf("IsPartialContent = %v, want %v", resp.IsPartialContent, tt.wantPartial)
			}

			if resp.AcceptRanges != "bytes" {
				t.Errorf("AcceptRanges = %q, want %q", resp.AcceptRanges, "bytes")
			}

			tt.checkResponse(t, len(resp.Bytes()))
		})
	}
}

func TestContentRangeParsing(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	opt := options.New().SetRange(100, 199)
	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.ContentRange == nil {
		t.Fatal("ContentRange is nil")
	}

	if resp.ContentRange.Unit != "bytes" {
		t.Errorf("ContentRange.Unit = %q, want %q", resp.ContentRange.Unit, "bytes")
	}

	if resp.ContentRange.Start != 100 {
		t.Errorf("ContentRange.Start = %d, want %d", resp.ContentRange.Start, 100)
	}

	if resp.ContentRange.End != 199 {
		t.Errorf("ContentRange.End = %d, want %d", resp.ContentRange.End, 199)
	}

	expectedTotal := int64(largefile.Len())
	if resp.ContentRange.Total != expectedTotal {
		t.Errorf("ContentRange.Total = %d, want %d", resp.ContentRange.Total, expectedTotal)
	}
}

func TestResume(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	dir := t.TempDir()
	pf := filepath.Join(dir, "partial.bin")
	ff := filepath.Join(dir, "full.bin")

	total := int64(largefile.Len())
	half := total / 2

	// Step 1: Download first half
	opt := options.New().
		SetRange(0, half-1).
		SetFileOutput(pf)

	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("initial download failed: %v", err)
	}
	if resp.StatusCode != 206 {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}

	info, err := os.Stat(pf)
	if err != nil {
		t.Fatalf("failed to stat partial file: %v", err)
	}
	if info.Size() != half {
		t.Fatalf("partial file size = %d, want %d", info.Size(), half)
	}

	// Step 2: Resume download from where we left off
	opt = options.New().Resume(pf, resp.Header.Get("ETag"))
	resp, err = c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("resume download failed: %v", err)
	}
	if resp.StatusCode != 206 {
		t.Fatalf("expected 206 on resume, got %d", resp.StatusCode)
	}

	info, err = os.Stat(pf)
	if err != nil {
		t.Fatalf("failed to stat completed file: %v", err)
	}
	if info.Size() != total {
		t.Fatalf("completed file size = %d, want %d", info.Size(), total)
	}

	// Step 3: Download full file in one go for comparison
	opt = options.New().SetFileOutput(ff)
	resp, err = c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("full download failed: %v", err)
	}

	pc, err := os.ReadFile(pf)
	if err != nil {
		t.Fatalf("failed to read partial file: %v", err)
	}
	fc, err := os.ReadFile(ff)
	if err != nil {
		t.Fatalf("failed to read full file: %v", err)
	}
	if string(pc) != string(fc) {
		t.Error("resumed file content doesn't match full download")
	}
}

func TestResumeFromNonExistentFile(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	f := filepath.Join(t.TempDir(), "new.bin")

	// Resume with non-existent file should start fresh (no Range header)
	opt := options.New().Resume(f, "")
	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Should get 200 OK (not 206) because no Range header was sent
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for fresh download, got %d", resp.StatusCode)
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	if info.Size() != int64(largefile.Len()) {
		t.Errorf("file size = %d, want %d", info.Size(), largefile.Len())
	}
}

func TestResumeFromEmptyFile(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	ef := filepath.Join(t.TempDir(), "empty.bin")

	// Create empty file
	f, err := os.Create(ef)
	if err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}
	f.Close()

	// Resume with empty file should start fresh
	opt := options.New().Resume(ef, "")
	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Should get 200 OK because no Range header was sent for empty file
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for fresh download, got %d", resp.StatusCode)
	}
}

func TestServerWithoutRangeSupport(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	opt := options.New().SetRange(0, 999)
	resp, err := c.Get(server.URL+"/download/no-range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.IsPartialContent {
		t.Error("expected IsPartialContent = false")
	}
	if resp.AcceptRanges != "none" {
		t.Errorf("AcceptRanges = %q, want %q", resp.AcceptRanges, "none")
	}
	if len(resp.Bytes()) != largefile.Len() {
		t.Errorf("body length = %d, want %d", len(resp.Bytes()), largefile.Len())
	}
}

func TestInvalidRange416Response(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	opt := options.New().SetRangeFrom(int64(largefile.Len()) + 1000)
	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 416 {
		t.Errorf("status = %d, want 416", resp.StatusCode)
	}
}

func TestHasRange(t *testing.T) {
	opt := options.New()

	if opt.HasRange() {
		t.Error("HasRange() should be false for new option")
	}

	opt.SetRange(0, 100)
	if !opt.HasRange() {
		t.Error("HasRange() should be true after SetRange")
	}

	opt.ClearRange()
	if opt.HasRange() {
		t.Error("HasRange() should be false after ClearRange")
	}
}

func TestRangeWithFileOutput(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	f := filepath.Join(t.TempDir(), "output.bin")

	opt := options.New().
		SetRange(1000, 1999).
		SetFileOutput(f)

	resp, err := c.Get(server.URL+"/download/range", opt)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 206 {
		t.Errorf("status = %d, want 206", resp.StatusCode)
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatalf("failed to stat output file: %v", err)
	}
	if info.Size() != 1000 {
		t.Errorf("file size = %d, want 1000", info.Size())
	}

	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	want := largefile.Bytes()[1000:2000]
	if string(got) != string(want) {
		t.Error("file content doesn't match expected range")
	}
}

// rangeRecorder returns a server that records the Range header of each request.
func rangeRecorder(t *testing.T) (*httptest.Server, func() string) {
	var mu sync.Mutex
	var last string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = r.Header.Get("Range")
		mu.Unlock()
	}))
	t.Cleanup(server.Close)
	return server, func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func TestRangeHeaderValues(t *testing.T) {
	server, received := rangeRecorder(t)

	tests := []struct {
		name string
		opt  *options.Option
		want string
	}{
		{"first byte", options.New().SetRange(0, 0), "bytes=0-0"},
		{"bounded", options.New().SetRange(10, 19), "bytes=10-19"},
		{"from offset", options.New().SetRangeFrom(0), "bytes=0-"},
		{"from later offset", options.New().SetRangeFrom(500), "bytes=500-"},
		{"last bytes", options.New().SetRangeLast(256), "bytes=-256"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := client.Get(server.URL, tt.opt); err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if got := received(); got != tt.want {
				t.Errorf("Range = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInvalidRangeIsRejected(t *testing.T) {
	server, _ := rangeRecorder(t)

	tests := []struct {
		name string
		opt  *options.Option
	}{
		{"negative start", options.New().SetRange(-1, 10)},
		{"end before start", options.New().SetRange(10, 5)},
		{"negative offset", options.New().SetRangeFrom(-1)},
		{"zero suffix", options.New().SetRangeLast(0)},
		{"negative suffix", options.New().SetRangeLast(-5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Get(server.URL, tt.opt)
			if !errors.Is(err, options.ErrInvalidRange) {
				t.Errorf("err = %v, want ErrInvalidRange", err)
			}
		})
	}
}

// resumeServer serves content with range support through http.ServeContent,
// with the given ETag, and records the Range and If-Range headers it receives.
type resumeServer struct {
	*httptest.Server
	mu       sync.Mutex
	content  string
	etag     string
	ranges   []string
	ifRanges []string
}

func newResumeServer(t *testing.T, content, etag string) *resumeServer {
	s := &resumeServer{content: content, etag: etag}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		s.ifRanges = append(s.ifRanges, r.Header.Get("If-Range"))
		content, etag := s.content, s.etag
		s.mu.Unlock()
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(content))
	}))
	t.Cleanup(s.Close)
	return s
}

// received returns the Range and If-Range headers of each request so far.
func (s *resumeServer) received() (ranges, ifRanges []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...), append([]string(nil), s.ifRanges...)
}

func TestResumeOptionReuseUsesCurrentFileSize(t *testing.T) {
	server := newResumeServer(t, "abcdef", `"v1"`)
	path := filepath.Join(t.TempDir(), "partial.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}

	opt := options.New().Resume(path, `"v1"`)
	for range 2 {
		if _, err := client.Get(server.URL, opt); err != nil {
			t.Fatalf("request failed: %v", err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcdef" {
		t.Errorf("file = %q, want %q", got, "abcdef")
	}
	ranges, _ := server.received()
	if want := []string{"bytes=3-", "bytes=6-"}; !slices.Equal(ranges, want) {
		t.Errorf("Range headers = %q, want %q", ranges, want)
	}
}

func TestResumeValidatesResponse(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		contentRange string
		body         string
		wantFile     string
		wantErr      error
	}{
		{"valid continuation", http.StatusPartialContent, "bytes 3-5/6", "def", "abcdef", nil},
		{"full response restarts the file", http.StatusOK, "", "abcdef", "abcdef", nil},
		{"wrong start", http.StatusPartialContent, "bytes 0-2/6", "abc", "abc", client.ErrRangeMismatch},
		{"unsatisfiable range", http.StatusRequestedRangeNotSatisfiable, "bytes */3", "range error", "abc", nil},
		{"server error", http.StatusInternalServerError, "", "server error", "abc", nil},
		{"body shorter than range", http.StatusPartialContent, "bytes 3-5/6", "d", "abcd", client.ErrRangeMismatch},
		{"body longer than range", http.StatusPartialContent, "bytes 3-5/6", "defgh", "abcdef", client.ErrRangeMismatch},
		{"missing Content-Range", http.StatusPartialContent, "", "def", "abc", client.ErrRangeMismatch},
		{"other range unit", http.StatusPartialContent, "items 3-5/6", "def", "abc", client.ErrRangeMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.contentRange != "" {
					w.Header().Set("Content-Range", tt.contentRange)
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(tt.body)))
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			path := filepath.Join(t.TempDir(), "partial.bin")
			if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := client.Get(server.URL, options.New().Resume(path, `"v1"`))
			if tt.wantErr == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}
		})
	}
}

func TestParseContentRangeValues(t *testing.T) {
	tests := []struct {
		header string
		want   *response.ContentRange
	}{
		{"bytes 0-499/1234", &response.ContentRange{Unit: "bytes", Start: 0, End: 499, Total: 1234}},
		{"bytes 500-999/*", &response.ContentRange{Unit: "bytes", Start: 500, End: 999, Total: -1}},
		{"bytes */3", &response.ContentRange{Unit: "bytes", Start: -1, End: -1, Total: 3}},
		{"bytes 5-2/10", nil},
		{"bytes 0-10/5", nil},
		{"bytes -1-2/5", nil},
		{"bytes */*", nil},
		{"bytes", nil},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got, err := response.ParseContentRange(tt.header)
			if tt.want == nil {
				if !errors.Is(err, response.ErrInvalidContentRange) {
					t.Errorf("err = %v, want ErrInvalidContentRange", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != *tt.want {
				t.Errorf("got %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestResumeRepresentationIdentity(t *testing.T) {
	tests := []struct {
		name        string
		local       string
		content     string
		etag        string
		validator   string
		wantRange   string
		wantIfRange string
		wantFile    string
	}{
		{"matching validator continues", "abc", "abcdef", `"v1"`, `"v1"`, "bytes=3-", `"v1"`, "abcdef"},
		{"changed resource replaces the file", "OLD", "abcnew", `"v2"`, `"v1"`, "bytes=3-", `"v1"`, "abcnew"},
		{"no validator starts again", "OLD", "abcnew", `"v2"`, "", "", "", "abcnew"},
		{"weak validator starts again", "OLD", "abcnew", `W/"v2"`, `W/"v2"`, "", "", "abcnew"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newResumeServer(t, tt.content, tt.etag)
			path := filepath.Join(t.TempDir(), "partial.bin")
			if err := os.WriteFile(path, []byte(tt.local), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := client.Get(server.URL, options.New().Resume(path, tt.validator)); err != nil {
				t.Fatalf("request failed: %v", err)
			}

			ranges, ifRanges := server.received()
			if ranges[0] != tt.wantRange {
				t.Errorf("Range = %q, want %q", ranges[0], tt.wantRange)
			}
			if ifRanges[0] != tt.wantIfRange {
				t.Errorf("If-Range = %q, want %q", ifRanges[0], tt.wantIfRange)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}
		})
	}
}

func TestResumeRejectsOtherRepresentation(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{"different ETag", http.Header{"Etag": {`"v2"`}}},
		{"encoded body", http.Header{"Content-Encoding": {"gzip"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acceptEncoding string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				acceptEncoding = r.Header.Get("Accept-Encoding")
				maps.Copy(w.Header(), tt.header)
				w.Header().Set("Content-Range", "bytes 3-5/6")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte("new"))
			}))
			defer server.Close()

			path := filepath.Join(t.TempDir(), "partial.bin")
			if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := client.Get(server.URL, options.New().Resume(path, `"v1"`))
			if !errors.Is(err, client.ErrRangeMismatch) {
				t.Errorf("err = %v, want ErrRangeMismatch", err)
			}
			if acceptEncoding != "identity" {
				t.Errorf("Accept-Encoding = %q, want identity", acceptEncoding)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "OLD" {
				t.Errorf("file = %q, want %q", got, "OLD")
			}
		})
	}
}
