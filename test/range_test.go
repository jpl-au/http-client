package client_test

import (
	"os"
	"path/filepath"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
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
	opt = options.New().Resume(pf)
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
	opt := options.New().Resume(f)
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
	opt := options.New().Resume(ef)
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
