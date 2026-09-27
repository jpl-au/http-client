package client_test

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/form"
	"github.com/jpl-au/http-client/options"
)

// TestOptionsMergeInitialised tests that Merge respects the initialised flag for boolean fields
func TestOptionsMergeInitialised(t *testing.T) {
	t.Run("Uninitialised source should not override booleans", func(t *testing.T) {
		// Create a properly initialised option with specific boolean values
		dest := options.New()
		dest.Logging.Enabled = true
		dest.Redirect.Follow = true

		// Create an uninitialised option (zero values for booleans)
		src := &options.Option{}

		// Merge - uninitialised source should NOT override dest booleans
		dest.Merge(src)

		if !dest.Logging.Enabled {
			t.Error("Logging.Enabled = false after merge with an uninitialised source, want true")
		}
		if !dest.Redirect.Follow {
			t.Error("Redirect.Follow = false after merge with an uninitialised source, want true")
		}
	})

	t.Run("Default source should not override booleans", func(t *testing.T) {
		dest := options.New()
		dest.Logging.Enabled = true
		dest.Redirect.Follow = true
		dest.TrackAfterCompression()
		dest.SetMaxRedirects(3)
		dest.SetIdentifierType(options.IdentifierUUID)
		dest.UserAgent = "custom-agent"
		dest.SetLogger(slog.New(slog.DiscardHandler))
		dest.SetFileOutput("download.bin")

		// A default source carries no choices, so it must not reset dest
		dest.Merge(options.New())

		if !dest.Logging.Enabled {
			t.Error("Logging.Enabled = false after merge with a default source, want true")
		}
		if !dest.Redirect.Follow {
			t.Error("Redirect.Follow = false after merge with a default source, want true")
		}
		if got := dest.ProgressTracking(); got != options.TrackAfterCompression {
			t.Errorf("ProgressTracking() = %d after merge with a default source, want %d", got, options.TrackAfterCompression)
		}
		if got := dest.MaxRedirects(); got != 3 {
			t.Errorf("MaxRedirects() = %d after merge with a default source, want 3", got)
		}
		if got := dest.IdentifierType(); got != options.IdentifierUUID {
			t.Errorf("IdentifierType() = %q after merge with a default source, want %q", got, options.IdentifierUUID)
		}
		if dest.UserAgent != "custom-agent" {
			t.Errorf("UserAgent = %q after merge with a default source, want %q", dest.UserAgent, "custom-agent")
		}
		if got := dest.Logging.Logger.Handler(); got != slog.DiscardHandler {
			t.Errorf("Logger.Handler() = %T after merge with a default source, want slog.DiscardHandler", got)
		}
		if dest.ResponseWriter.Type != options.WriteToFile {
			t.Errorf("ResponseWriter.Type = %q after merge with a default source, want %q", dest.ResponseWriter.Type, options.WriteToFile)
		}
	})

	t.Run("Setters that choose a zero value should override", func(t *testing.T) {
		dest := options.New().
			EnableLogging().
			EnableRedirects().
			TrackAfterCompression().
			SetCompression(options.CompressionGzip).
			SetIdentifierType(options.IdentifierUUID).
			SetProtocol(options.HTTP1).
			SetMaxResponseHeaderBytes(1024).
			SetRange(0, 99).
			SetFileOutput("download.bin")

		src := options.New().
			DisableLogging().
			DisableRedirects().
			TrackBeforeCompression().
			SetCompression(options.CompressionNone).
			SetIdentifierType(options.IdentifierNone).
			SetProtocol(options.Both).
			SetMaxResponseHeaderBytes(0).
			ClearRange().
			SetBufferOutput()

		dest.Merge(src)

		if dest.Logging.Enabled {
			t.Error("Logging.Enabled = true, want false")
		}
		if dest.Redirect.Follow {
			t.Error("Redirect.Follow = true, want false")
		}
		if got := dest.ProgressTracking(); got != options.TrackBeforeCompression {
			t.Errorf("ProgressTracking() = %d, want %d", got, options.TrackBeforeCompression)
		}
		if dest.Compression.Type != options.CompressionNone {
			t.Errorf("Compression.Type = %q, want %q", dest.Compression.Type, options.CompressionNone)
		}
		if got := dest.IdentifierType(); got != options.IdentifierNone {
			t.Errorf("IdentifierType() = %q, want %q", got, options.IdentifierNone)
		}
		if dest.Transport.Protocol != options.Both {
			t.Errorf("Transport.Protocol = %d, want %d", dest.Transport.Protocol, options.Both)
		}
		if dest.Transport.MaxResponseHeaderBytes != 0 {
			t.Errorf("Transport.MaxResponseHeaderBytes = %d, want 0", dest.Transport.MaxResponseHeaderBytes)
		}
		if dest.HasRange() {
			t.Error("HasRange() = true, want false")
		}
		if dest.ResponseWriter.Type != options.WriteToBuffer {
			t.Errorf("ResponseWriter.Type = %q, want %q", dest.ResponseWriter.Type, options.WriteToBuffer)
		}
	})

	t.Run("MaxRedirects zero should not override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := &options.Option{} // uninitialised, MaxRedirects = 0

		dest.Merge(src)

		if dest.Redirect.Max != 15 {
			t.Errorf("Redirect.Max = %d after merge with a zero value, want 15", dest.Redirect.Max)
		}
	})

	t.Run("MaxRedirects non-zero should override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := options.New()
		src.Redirect.Max = 5

		dest.Merge(src)

		if dest.Redirect.Max != 5 {
			t.Errorf("Redirect.Max = %d after merge, want 5", dest.Redirect.Max)
		}
	})
}

// TestReusedOptionSendsCookiesOnce checks that reusing one Option for several
// package-level requests does not accumulate cookies.
func TestReusedOptionSendsCookiesOnce(t *testing.T) {
	var cookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
	}))
	defer server.Close()

	opt := options.New().AddCookie(&http.Cookie{Name: "session", Value: "audit"})
	for range 3 {
		_, err := client.Get(server.URL, opt)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
	}

	if want := []string{"session=audit", "session=audit", "session=audit"}; !slices.Equal(cookies, want) {
		t.Errorf("Cookie headers = %q, want %q", cookies, want)
	}
}

// TestPackageFunctionsLeaveOptionUnchanged checks that package-level functions
// do not write request state into the caller's Option.
func TestPackageFunctionsLeaveOptionUnchanged(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	opt := options.New()

	_, err := client.Get(server.URL+"/echo", opt)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_, err = client.Post(server.URL+"/echo", url.Values{"k": {"v"}}, opt)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	_, err = client.PostFile(server.URL+"/upload", smallf, opt)
	if err != nil {
		t.Fatalf("PostFile() error = %v", err)
	}
	_, err = client.Post(server.URL+"/upload/multipart", form.New().Field("k", "v"), opt)
	if err != nil {
		t.Fatalf("Post() with a form error = %v", err)
	}

	if len(opt.Header) != 0 {
		t.Errorf("Option.Header = %v, want empty (request headers must not be written into the caller's Option)", opt.Header)
	}
	if opt.HasFile() {
		t.Error("Option.HasFile() = true, want false (PostFile must not prepare a file on the caller's Option)")
	}
}

// TestPackageFunctionsUseOptionClient checks that a client set on an Option
// carries package-level requests.
func TestPackageFunctionsUseOptionClient(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	var dials atomic.Int32
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials.Add(1)
			return dialer.DialContext(ctx, network, addr)
		},
	}
	defer transport.CloseIdleConnections()

	_, err := client.Get(server.URL+"/echo", options.New().SetClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got := dials.Load(); got != 1 {
		t.Errorf("dials = %d, want 1", got)
	}
}

// TestRedirectsZeroUsesDefaultMax checks that Redirects with a zero maximum
// falls back to the same limit as a new Option.
func TestRedirectsZeroUsesDefaultMax(t *testing.T) {
	if got, want := options.New().Redirects(true, 0).MaxRedirects(), options.New().MaxRedirects(); got != want {
		t.Errorf("Redirects(true, 0).MaxRedirects() = %d, want %d", got, want)
	}
}

// TestMergeResetSetters checks that setters that clear a setting override the
// destination through Merge, and that a default source leaves those settings.
func TestMergeResetSetters(t *testing.T) {
	base := func() *options.Option {
		return options.New().
			SetContext(context.Background()).
			OnUploadProgress(func(int64, int64) {}).
			OnDownloadProgress(func(int64, int64) {}).
			SetTransport(&http.Transport{}).
			SetProtocolScheme("https").
			SetMaxBodySize(1024).
			SetStallTimeout(time.Second).
			SetChecksum(sha256.New, "00")
	}

	t.Run("default source keeps the settings", func(t *testing.T) {
		dest := base()
		dest.Merge(options.New())

		if dest.Context == nil {
			t.Error("Context = nil, want the context kept")
		}
		if dest.Progress.OnUpload == nil {
			t.Error("Progress.OnUpload = nil, want the callback kept")
		}
		if dest.Progress.OnDownload == nil {
			t.Error("Progress.OnDownload = nil, want the callback kept")
		}
		if dest.Transport.HTTP == nil {
			t.Error("Transport.HTTP = nil, want the transport kept")
		}
		if dest.Transport.Scheme != "https://" {
			t.Errorf("Transport.Scheme = %q, want %q", dest.Transport.Scheme, "https://")
		}
		if dest.MaxBodySize != 1024 {
			t.Errorf("MaxBodySize = %d, want 1024", dest.MaxBodySize)
		}
		if dest.StallTimeout != time.Second {
			t.Errorf("StallTimeout = %v, want %v", dest.StallTimeout, time.Second)
		}
		if dest.Checksum.New == nil || dest.Checksum.Expected != "00" {
			t.Errorf("Checksum = %+v, want sha256.New and %q", dest.Checksum, "00")
		}
	})

	t.Run("reset setters clear the settings", func(t *testing.T) {
		dest := base()
		dest.Merge(options.New().
			SetContext(nil).
			OnUploadProgress(nil).
			OnDownloadProgress(nil).
			SetTransport(nil).
			SetProtocolScheme("").
			SetMaxBodySize(0).
			SetStallTimeout(0).
			SetChecksum(nil, ""))

		if dest.Context != nil {
			t.Errorf("Context = %v, want nil", dest.Context)
		}
		if dest.Progress.OnUpload != nil {
			t.Error("Progress.OnUpload is set, want nil")
		}
		if dest.Progress.OnDownload != nil {
			t.Error("Progress.OnDownload is set, want nil")
		}
		if dest.Transport.HTTP != nil {
			t.Errorf("Transport.HTTP = %v, want nil", dest.Transport.HTTP)
		}
		if dest.Transport.Scheme != "" {
			t.Errorf("Transport.Scheme = %q, want \"\"", dest.Transport.Scheme)
		}
		if dest.MaxBodySize != 0 {
			t.Errorf("MaxBodySize = %d, want 0", dest.MaxBodySize)
		}
		if dest.StallTimeout != 0 {
			t.Errorf("StallTimeout = %v, want 0", dest.StallTimeout)
		}
		if dest.Checksum.New != nil || dest.Checksum.Expected != "" {
			t.Errorf("Checksum = %+v, want none", dest.Checksum)
		}
	})
}
