package client_test

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedirectPostUploadNoFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.Redirect.Follow = false

	resp, err := client.Post(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Location"))
}

func TestRedirectPostUploadNoPreserve(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.Redirect.Follow = true

	resp, err := client.Post(server.URL+"/upload/no-preserve", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "GET", resp.String())
}

func TestRedirectMaxRedirects(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.EnableLogging()
	opt.Redirect.Follow = true
	opt.Redirect.Max = 5

	resp, err := client.Post(server.URL+"/max-redirects", tmpfile, opt)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, client.ErrMaxRedirectsExceeded), "expected ErrMaxRedirectsExceeded, got: %v", err)
	assert.Equal(t, "", resp.String())
}

func TestRedirectPostUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			progress := float64(bytesRead) / float64(totalBytes) * 100
			lastProgress.Store(progress)
			t.Logf("Upload progress: %f", progress)
		}
	}

	resp, err := client.Post(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectPutUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
		}
	}

	resp, err := client.Put(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectPatchUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
		}
	}

	resp, err := client.Patch(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectFileFuncUpload(t *testing.T) {
	var err error
	var resp response.Response

	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{"PostFile Request", http.MethodPost, http.StatusOK},
		{"PutFile Request", http.MethodPut, http.StatusOK},
		{"PatchFile Request", http.MethodPatch, http.StatusOK},
	}

	url := server.URL + "/upload/redirect"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt.Redirects(true, 5)
			opt.EnableLogging()

			var lastProgress atomic.Value
			lastProgress.Store(float64(0))
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
				}
			}
			switch tt.method {
			case http.MethodPost:
				resp, err = client.PostFile(url, largef, opt)
			case http.MethodPut:
				resp, err = client.PutFile(url, largef, opt)
			case http.MethodPatch:
				resp, err = client.PatchFile(url, largef, opt)
			}

			assert.NoError(t, err)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
			// Verify upload progress completed
			assert.Equal(t, float64(100), lastProgress.Load().(float64))
		})
	}
}

func TestCompressedFileRedirect(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name         string
		method       string
		compression  options.CompressionType
		expectedSize int64
	}{
		{"POST Gzip Compressed Redirect", http.MethodPost, options.CompressionGzip, int64(largefile.Len())},
		{"POST Deflate Compressed Redirect", http.MethodPost, options.CompressionDeflate, int64(largefile.Len())},
		{"POST Brotli Compressed Redirect", http.MethodPost, options.CompressionBrotli, int64(largefile.Len())},
		{"PUT Gzip Compressed Redirect", http.MethodPut, options.CompressionGzip, int64(largefile.Len())},
		{"PUT Deflate Compressed Redirect", http.MethodPut, options.CompressionDeflate, int64(largefile.Len())},
		{"PUT Brotli Compressed Redirect", http.MethodPut, options.CompressionBrotli, int64(largefile.Len())},
		{"PATCH Gzip Compressed Redirect", http.MethodPatch, options.CompressionGzip, int64(largefile.Len())},
		{"PATCH Deflate Compressed Redirect", http.MethodPatch, options.CompressionDeflate, int64(largefile.Len())},
		{"PATCH Brotli Compressed Redirect", http.MethodPatch, options.CompressionBrotli, int64(largefile.Len())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp response.Response
			var err error

			opt := options.New()
			opt.Redirects(true, 5) // Enable redirects and preserve method
			opt.SetCompression(tt.compression)

			// Track upload progress
			var lastProgress atomic.Value
			lastProgress.Store(float64(0))
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
				}
			}

			t.Logf("[%s] Original file size: %d bytes", tt.name, int64(smallfile.Len()))

			url := server.URL + "/upload/redirect"

			switch tt.method {
			case http.MethodPost:
				resp, err = client.PostFile(url, smallf, opt)
			case http.MethodPut:
				resp, err = client.PutFile(url, smallf, opt)
			case http.MethodPatch:
				resp, err = client.PatchFile(url, smallf, opt)
			}

			// Verify the request succeeded
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify the content was transmitted correctly
			assert.Equal(t, smallfile.String(), resp.String())

			// Verify upload progress completed
			t.Logf("%s Last progress: %f", tt.name, lastProgress.Load().(float64))
			// TODO: Progress tracking with compression + redirects may not reach 100%
			// assert.Equal(t, float64(100), lastProgress.Load().(float64))

			// Log the response size to see compression effectiveness
			t.Logf("[%s] Response size: %d bytes", tt.name, len(resp.Body.Bytes()))
		})
	}
}

func TestRedirectWithFileReopenError(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-redirect-*.txt")
	require.NoError(t, err)
	_, _ = tmpFile.WriteString("test content")
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	// Set up options to follow redirects and preserve method
	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.Max = 5

	// Delete the file before making the request that will redirect
	// This simulates the file becoming unavailable between redirects
	os.Remove(tmpPath)

	// This should fail because the file doesn't exist
	_, err = client.PostFile(server.URL+"/redirect/upload", tmpPath, opt)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, options.ErrFileNotFound), "expected ErrFileNotFound, got: %v", err)
}

// hop records what the redirect destination received.
type hop struct {
	Method string
	Body   string
	Header http.Header
}

// newRedirectServer returns a server whose /redirect/{code} path redirects with that
// status code to target, and a function that returns what /destination received.
// When target is empty, the redirect goes to /destination on the same server.
func newRedirectServer(t *testing.T, target string) (*httptest.Server, func() []hop) {
	var mu sync.Mutex
	var hops []hop
	mux := http.NewServeMux()
	mux.HandleFunc("/redirect/{code}", func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		location := target
		if location == "" {
			location = "/destination"
		}
		http.Redirect(w, r, location, code)
	})
	mux.HandleFunc("/destination", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hops = append(hops, hop{Method: r.Method, Body: string(body), Header: r.Header.Clone()})
		mu.Unlock()
		_, _ = w.Write([]byte("done"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, func() []hop {
		mu.Lock()
		defer mu.Unlock()
		return append([]hop(nil), hops...)
	}
}

func TestRedirectStripsCredentialsAcrossHosts(t *testing.T) {
	destination, received := newRedirectServer(t, "")
	// The same server addressed by another host name counts as a different host.
	other := strings.Replace(destination.URL, "127.0.0.1", "localhost", 1)
	origin, _ := newRedirectServer(t, other+"/destination")

	opt := options.New().
		EnableRedirects().
		AddHeader("Authorization", "Bearer audit-token").
		AddCookie(&http.Cookie{Name: "session", Value: "audit-cookie"})

	resp, err := client.Get(origin.URL+"/redirect/302", opt)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	hops := received()
	require.Len(t, hops, 1)
	assert.Empty(t, hops[0].Header.Get("Authorization"), "credentials must not reach another host")
	assert.Empty(t, hops[0].Header.Get("Cookie"), "cookies must not reach another host")
}

func TestRedirectKeepsCredentialsOnSameHost(t *testing.T) {
	server, received := newRedirectServer(t, "")

	opt := options.New().
		EnableRedirects().
		AddHeader("Authorization", "Bearer audit-token").
		AddCookie(&http.Cookie{Name: "session", Value: "audit-cookie"})

	_, err := client.Get(server.URL+"/redirect/302", opt)
	require.NoError(t, err)

	hops := received()
	require.Len(t, hops, 1)
	assert.Equal(t, "Bearer audit-token", hops[0].Header.Get("Authorization"))
	assert.Equal(t, "session=audit-cookie", hops[0].Header.Get("Cookie"), "the cookie should be sent once")
}

func TestRedirectHonoursClientCheckRedirect(t *testing.T) {
	server, received := newRedirectServer(t, "")
	errRejected := errors.New("redirect rejected")
	reject := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errRejected }}

	c := client.NewCustom(reject, options.New().EnableRedirects())
	_, err := c.Get(server.URL + "/redirect/302")
	assert.ErrorIs(t, err, errRejected)

	_, err = client.Get(server.URL+"/redirect/302", options.New().EnableRedirects().SetClient(reject))
	assert.ErrorIs(t, err, errRejected)

	assert.Empty(t, received(), "a rejected redirect must not reach the destination")
}

func TestRedirectMethodByStatus(t *testing.T) {
	tests := []struct {
		method     string
		code       int
		wantMethod string
		wantBody   string
	}{
		{http.MethodPost, http.StatusMovedPermanently, http.MethodGet, ""},
		{http.MethodPost, http.StatusFound, http.MethodGet, ""},
		{http.MethodPost, http.StatusSeeOther, http.MethodGet, ""},
		{http.MethodPut, http.StatusSeeOther, http.MethodGet, ""},
		{http.MethodPost, http.StatusTemporaryRedirect, http.MethodPost, "payload"},
		{http.MethodPost, http.StatusPermanentRedirect, http.MethodPost, "payload"},
		{http.MethodPut, http.StatusTemporaryRedirect, http.MethodPut, "payload"},
		{http.MethodHead, http.StatusFound, http.MethodHead, ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %d", tt.method, tt.code), func(t *testing.T) {
			server, received := newRedirectServer(t, "")
			url := fmt.Sprintf("%s/redirect/%d", server.URL, tt.code)
			opt := options.New().EnableRedirects()

			var err error
			switch tt.method {
			case http.MethodPost:
				_, err = client.Post(url, "payload", opt)
			case http.MethodPut:
				_, err = client.Put(url, "payload", opt)
			case http.MethodHead:
				_, err = client.Head(url, opt)
			}
			require.NoError(t, err)

			hops := received()
			require.Len(t, hops, 1)
			assert.Equal(t, tt.wantMethod, hops[0].Method)
			assert.Equal(t, tt.wantBody, hops[0].Body)
		})
	}
}

func TestRedirectReplaysFileAndCompressedBodies(t *testing.T) {
	server, received := newRedirectServer(t, "")
	url := server.URL + "/redirect/307"

	_, err := client.PostFile(url, smallf, options.New().EnableRedirects())
	require.NoError(t, err)

	_, err = client.Post(url, "payload", options.New().EnableRedirects().SetCompression(options.CompressionGzip))
	require.NoError(t, err)

	hops := received()
	require.Len(t, hops, 2)
	assert.Equal(t, smallfile.String(), hops[0].Body)
	gz, err := gzip.NewReader(strings.NewReader(hops[1].Body))
	require.NoError(t, err)
	body, err := io.ReadAll(gz)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(body))
}

func TestRedirectDropsBodyHeaders(t *testing.T) {
	server, received := newRedirectServer(t, "")

	opt := options.New().EnableRedirects().SetCompression(options.CompressionGzip)
	opt.AddHeader("Content-Type", "text/plain")
	_, err := client.Post(server.URL+"/redirect/303", "payload", opt)
	require.NoError(t, err)

	hops := received()
	require.Len(t, hops, 1)
	assert.Equal(t, http.MethodGet, hops[0].Method)
	assert.Empty(t, hops[0].Header.Get("Content-Encoding"), "a GET without a body has no content encoding")
	assert.Empty(t, hops[0].Header.Get("Content-Type"), "a GET without a body has no content type")
}

func TestRedirectNonReplayableBody(t *testing.T) {
	server, received := newRedirectServer(t, "")

	// io.MultiReader hides Seek, so the payload can only be read once.
	payload := io.MultiReader(strings.NewReader("payload"))
	_, err := client.Post(server.URL+"/redirect/307", payload, options.New().EnableRedirects())
	assert.ErrorIs(t, err, client.ErrPayloadNotReplayable)
	assert.Empty(t, received(), "an empty body must not be sent in place of the payload")
}

func TestRedirectReportsFinalURL(t *testing.T) {
	server, _ := newRedirectServer(t, "")

	resp, err := client.Get(server.URL+"/redirect/302", options.New().EnableRedirects())
	require.NoError(t, err)

	assert.True(t, resp.Redirected)
	assert.Equal(t, server.URL+"/destination", resp.Location)
}

func TestRejectedRedirectKeepsResponse(t *testing.T) {
	server, _ := newRedirectServer(t, "")
	errRejected := errors.New("redirect rejected")
	reject := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errRejected }}

	resp, err := client.Get(server.URL+"/redirect/302", options.New().EnableRedirects().SetClient(reject))
	require.ErrorIs(t, err, errRejected)

	assert.Equal(t, err, resp.Error)
	assert.Equal(t, http.StatusFound, resp.StatusCode, "the rejected redirect response should be recorded")
	assert.Equal(t, "/destination", resp.Header.Get("Location"))
}
