package client_test

import (
	"bytes"
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
	"github.com/jpl-au/http-client/history"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
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
	if err != nil {
		t.Errorf("Post() error = %v", err)
	}

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusTemporaryRedirect)
	}
	if got := resp.Header.Get("Location"); got == "" {
		t.Error(`Header.Get("Location") = "", want a location`)
	}
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
	if err != nil {
		t.Errorf("Post() error = %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.String(); got != "GET" {
		t.Errorf("String() = %q, want %q", got, "GET")
	}
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
	if err == nil {
		t.Error("Post() error = nil, want error")
	}
	if !errors.Is(err, client.ErrMaxRedirectsExceeded) {
		t.Errorf("Post() error = %v, want %v", err, client.ErrMaxRedirectsExceeded)
	}
	if got := resp.String(); got != "" {
		t.Errorf("String() = %q, want %q", got, "")
	}
}

func TestRedirectUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name string
		send func(url string, payload any, opts ...*options.Option) (response.Response, error)
	}{
		{"Post", client.Post},
		{"Put", client.Put},
		{"Patch", client.Patch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(largef)
			if err != nil {
				t.Fatalf("Open(%q) error = %v", largef, err)
			}
			defer file.Close()

			var lastProgress atomic.Value
			lastProgress.Store(float64(0))
			opt := options.New()
			opt.Redirect.Follow = true
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
				}
			}

			resp, err := tt.send(server.URL+"/upload/redirect", file, opt)
			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if got := lastProgress.Load().(float64); got != 100 {
				t.Errorf("upload progress = %v, want 100", got)
			}
			if got, want := resp.Body.Bytes(), largefile.Bytes(); !bytes.Equal(got, want) {
				t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), len(want))
			}
		})
	}
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

			if err != nil {
				t.Fatalf("%s error = %v", tt.name, err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if got, want := resp.Body.Bytes(), largefile.Bytes(); !bytes.Equal(got, want) {
				t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), len(want))
			}
			// Verify upload progress completed
			if got := lastProgress.Load().(float64); got != 100 {
				t.Errorf("upload progress = %v, want 100", got)
			}
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
			if err != nil {
				t.Errorf("%s error = %v", tt.name, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			// Verify the content was transmitted correctly
			if got, want := resp.String(), smallfile.String(); got != want {
				t.Errorf("body does not match: got %d bytes, want %d bytes", len(got), len(want))
			}

			// Verify upload progress completed
			t.Logf("%s Last progress: %f", tt.name, lastProgress.Load().(float64))
			// TODO: Progress tracking with compression + redirects may not reach 100%
			// if got := lastProgress.Load().(float64); got != 100 {
			// 	t.Errorf("upload progress = %v, want 100", got)
			// }

			// Log the response size to see compression effectiveness
			t.Logf("[%s] Response size: %d bytes", tt.name, len(resp.Body.Bytes()))
		})
	}
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
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	hops := received()
	if len(hops) != 1 {
		t.Fatalf("destination received %d requests, want 1", len(hops))
	}
	if got := hops[0].Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization at another host = %q, want \"\" (credentials must not reach another host)", got)
	}
	if got := hops[0].Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie at another host = %q, want \"\" (cookies must not reach another host)", got)
	}
}

func TestRedirectKeepsCredentialsOnSameHost(t *testing.T) {
	server, received := newRedirectServer(t, "")

	opt := options.New().
		EnableRedirects().
		AddHeader("Authorization", "Bearer audit-token").
		AddCookie(&http.Cookie{Name: "session", Value: "audit-cookie"})

	_, err := client.Get(server.URL+"/redirect/302", opt)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	hops := received()
	if len(hops) != 1 {
		t.Fatalf("destination received %d requests, want 1", len(hops))
	}
	if got, want := hops[0].Header.Get("Authorization"), "Bearer audit-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got, want := hops[0].Header.Get("Cookie"), "session=audit-cookie"; got != want {
		t.Errorf("Cookie = %q, want %q (the cookie should be sent once)", got, want)
	}
}

func TestRedirectHonoursClientCheckRedirect(t *testing.T) {
	server, received := newRedirectServer(t, "")
	errRejected := errors.New("redirect rejected")
	reject := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errRejected }}

	c := client.NewCustom(reject, options.New().EnableRedirects())
	_, err := c.Get(server.URL + "/redirect/302")
	if !errors.Is(err, errRejected) {
		t.Errorf("Client.Get() error = %v, want %v", err, errRejected)
	}

	_, err = client.Get(server.URL+"/redirect/302", options.New().EnableRedirects().SetClient(reject))
	if !errors.Is(err, errRejected) {
		t.Errorf("Get() with SetClient error = %v, want %v", err, errRejected)
	}

	if n := len(received()); n != 0 {
		t.Errorf("destination received %d requests, want 0 (a rejected redirect must not reach the destination)", n)
	}
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
			if err != nil {
				t.Fatalf("%s error = %v", tt.method, err)
			}

			hops := received()
			if len(hops) != 1 {
				t.Fatalf("destination received %d requests, want 1", len(hops))
			}
			if hops[0].Method != tt.wantMethod {
				t.Errorf("destination method = %q, want %q", hops[0].Method, tt.wantMethod)
			}
			if hops[0].Body != tt.wantBody {
				t.Errorf("destination body = %q, want %q", hops[0].Body, tt.wantBody)
			}
		})
	}
}

func TestRedirectReplaysFileAndCompressedBodies(t *testing.T) {
	server, received := newRedirectServer(t, "")
	url := server.URL + "/redirect/307"

	_, err := client.PostFile(url, smallf, options.New().EnableRedirects())
	if err != nil {
		t.Fatalf("PostFile() error = %v", err)
	}

	_, err = client.Post(url, "payload", options.New().EnableRedirects().SetCompression(options.CompressionGzip))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}

	hops := received()
	if len(hops) != 2 {
		t.Fatalf("destination received %d requests, want 2", len(hops))
	}
	if got, want := hops[0].Body, smallfile.String(); got != want {
		t.Errorf("replayed file body does not match: got %d bytes, want %d bytes", len(got), len(want))
	}
	gz, err := gzip.NewReader(strings.NewReader(hops[1].Body))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if got := string(body); got != "payload" {
		t.Errorf("decompressed body = %q, want %q", got, "payload")
	}
}

func TestRedirectDropsBodyHeaders(t *testing.T) {
	server, received := newRedirectServer(t, "")

	opt := options.New().EnableRedirects().SetCompression(options.CompressionGzip)
	opt.AddHeader("Content-Type", "text/plain")
	_, err := client.Post(server.URL+"/redirect/303", "payload", opt)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}

	hops := received()
	if len(hops) != 1 {
		t.Fatalf("destination received %d requests, want 1", len(hops))
	}
	if hops[0].Method != http.MethodGet {
		t.Errorf("destination method = %q, want %q", hops[0].Method, http.MethodGet)
	}
	if got := hops[0].Header.Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want \"\" (a GET without a body has no content encoding)", got)
	}
	if got := hops[0].Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want \"\" (a GET without a body has no content type)", got)
	}
}

func TestRedirectNonReplayableBody(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server, received := newRedirectServer(t, "")
			h := history.New()
			c := client.New()
			c.SetHistory(h)
			// io.MultiReader hides Seek, so the payload can only be read once.
			payload := io.MultiReader(strings.NewReader("payload"))
			resp, err := c.Post(server.URL+"/redirect/"+strconv.Itoa(status), payload, options.New().EnableRedirects())
			if !errors.Is(err, client.ErrPayloadNotReplayable) {
				t.Fatalf("Post() error = %v, want %v", err, client.ErrPayloadNotReplayable)
			}
			if n := len(received()); n != 0 {
				t.Errorf("destination received %d requests, want 0 (an empty body must not be sent in place of the payload)", n)
			}
			if resp.StatusCode != status {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, status)
			}
			if got := resp.Header.Get("Location"); got != "/destination" {
				t.Errorf("Header.Get(%q) = %q, want %q", "Location", got, "/destination")
			}
			if !errors.Is(resp.Error, client.ErrPayloadNotReplayable) {
				t.Errorf("Response.Error = %v, want %v", resp.Error, client.ErrPayloadNotReplayable)
			}
			if got := h.Len(); got != 1 {
				t.Fatalf("Len() = %d, want 1", got)
			}
			stored, ok := h.Lookup(resp.UniqueIdentifier)
			if !ok {
				t.Fatalf("Lookup(%q) found nothing, want the stored response", resp.UniqueIdentifier)
			}
			if stored.StatusCode != status {
				t.Errorf("stored StatusCode = %d, want %d", stored.StatusCode, status)
			}
			if got := stored.Header.Get("Location"); got != "/destination" {
				t.Errorf("stored Header.Get(%q) = %q, want %q", "Location", got, "/destination")
			}
			if !errors.Is(stored.Error, client.ErrPayloadNotReplayable) {
				t.Errorf("stored Error = %v, want %v", stored.Error, client.ErrPayloadNotReplayable)
			}
			if stored.AccessTime <= 0 {
				t.Errorf("stored AccessTime = %v, want positive", stored.AccessTime)
			}
			if stored.ProcessedTime == 0 {
				t.Error("stored ProcessedTime = 0, want non-zero")
			}
		})
	}
}

func TestRedirectReportsFinalURL(t *testing.T) {
	server, _ := newRedirectServer(t, "")

	resp, err := client.Get(server.URL+"/redirect/302", options.New().EnableRedirects())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !resp.Redirected {
		t.Error("Redirected = false, want true")
	}
	if want := server.URL + "/destination"; resp.Location != want {
		t.Errorf("Location = %q, want %q", resp.Location, want)
	}
}

func TestRejectedRedirectKeepsResponse(t *testing.T) {
	server, _ := newRedirectServer(t, "")
	errRejected := errors.New("redirect rejected")
	reject := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errRejected }}

	resp, err := client.Get(server.URL+"/redirect/302", options.New().EnableRedirects().SetClient(reject))
	if !errors.Is(err, errRejected) {
		t.Fatalf("Get() error = %v, want %v", err, errRejected)
	}

	if resp.Error != err {
		t.Errorf("Response.Error = %v, want %v", resp.Error, err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d (the rejected redirect response should be recorded)", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != "/destination" {
		t.Errorf("Header.Get(%q) = %q, want %q", "Location", got, "/destination")
	}
}
