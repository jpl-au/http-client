package client_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		assert.True(t, dest.Logging.Enabled, "Logging.Enabled should remain true after merge with uninitialised source")
		assert.True(t, dest.Redirect.Follow, "FollowRedirects should remain true after merge with uninitialised source")
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

		assert.True(t, dest.Logging.Enabled, "Logging.Enabled should remain true after merge with a default source")
		assert.True(t, dest.Redirect.Follow, "FollowRedirects should remain true after merge with a default source")
		assert.Equal(t, options.TrackAfterCompression, dest.ProgressTracking(), "Tracking should remain after merge with a default source")
		assert.Equal(t, 3, dest.MaxRedirects(), "MaxRedirects should remain after merge with a default source")
		assert.Equal(t, options.IdentifierUUID, dest.IdentifierType(), "IdentifierType should remain after merge with a default source")
		assert.Equal(t, "custom-agent", dest.UserAgent, "UserAgent should remain after merge with a default source")
		assert.Equal(t, slog.DiscardHandler, dest.Logging.Logger.Handler(), "Logger should remain after merge with a default source")
		assert.Equal(t, options.WriteToFile, dest.ResponseWriter.Type, "file output should remain after merge with a default source")
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

		assert.False(t, dest.Logging.Enabled)
		assert.False(t, dest.Redirect.Follow)
		assert.Equal(t, options.TrackBeforeCompression, dest.ProgressTracking())
		assert.Equal(t, options.CompressionNone, dest.Compression.Type)
		assert.Equal(t, options.IdentifierNone, dest.IdentifierType())
		assert.Equal(t, options.Both, dest.Transport.Protocol)
		assert.Zero(t, dest.Transport.MaxResponseHeaderBytes)
		assert.False(t, dest.HasRange())
		assert.Equal(t, options.WriteToBuffer, dest.ResponseWriter.Type)
	})

	t.Run("MaxRedirects zero should not override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := &options.Option{} // uninitialised, MaxRedirects = 0

		dest.Merge(src)

		assert.Equal(t, 15, dest.Redirect.Max, "MaxRedirects should remain 15 after merge with zero value")
	})

	t.Run("MaxRedirects non-zero should override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := options.New()
		src.Redirect.Max = 5

		dest.Merge(src)

		assert.Equal(t, 5, dest.Redirect.Max, "MaxRedirects should be 5 after merge")
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
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"session=audit", "session=audit", "session=audit"}, cookies)
}

// TestPackageFunctionsLeaveOptionUnchanged checks that package-level functions
// do not write request state into the caller's Option.
func TestPackageFunctionsLeaveOptionUnchanged(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	opt := options.New()

	_, err := client.Get(server.URL+"/echo", opt)
	require.NoError(t, err)
	_, err = client.PostFormData(server.URL+"/echo", map[string]string{"k": "v"}, opt)
	require.NoError(t, err)
	_, err = client.PostFile(server.URL+"/upload", smallf, opt)
	require.NoError(t, err)
	_, err = client.PostMultipartUpload(server.URL+"/upload/multipart", map[string]any{"k": "v"}, opt)
	require.NoError(t, err)

	assert.Empty(t, opt.Header, "request headers should not be written into the caller's Option")
	assert.False(t, opt.HasFile(), "PostFile should not prepare a file on the caller's Option")
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
	require.NoError(t, err)

	assert.Equal(t, int32(1), dials.Load())
}

// TestRedirectsZeroUsesDefaultMax checks that Redirects with a zero maximum
// falls back to the same limit as a new Option.
func TestRedirectsZeroUsesDefaultMax(t *testing.T) {
	assert.Equal(t, options.New().MaxRedirects(), options.New().Redirects(true, 0).MaxRedirects())
}
