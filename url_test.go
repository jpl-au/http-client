package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

func TestNormaliseURL(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := client.Get("", nil)
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if !errors.Is(err, client.ErrEmptyURL) {
			t.Errorf("Get() error = %v, want %v", err, client.ErrEmptyURL)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		_, err := client.Get("   ", nil)
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if !errors.Is(err, client.ErrEmptyURL) {
			t.Errorf("Get() error = %v, want %v", err, client.ErrEmptyURL)
		}
	})

	// A URL without a scheme uses https, unless SetProtocolScheme sets another.
	// The URLs name example.com, which the test certificate covers, and the
	// transport dials the test server for every address.
	t.Run("no scheme uses https", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()

		c := client.NewCustom(clientDialling(server))
		resp, err := c.Get("example.com")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("no scheme uses set scheme", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()

		c := client.NewCustom(clientDialling(server))
		resp, err := c.Get("example.com", options.New().SetProtocolScheme("http"))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("with scheme", func(t *testing.T) {
		server := setupTestServer(t)
		defer server.Close()

		resp, err := client.Get(server.URL+"/", nil)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, 200)
		}
	})

	t.Run("with path", func(t *testing.T) {
		server := setupTestServer(t)
		defer server.Close()

		resp, err := client.Get(server.URL+"/echo-headers", nil)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, 200)
		}
	})

	t.Run("with query", func(t *testing.T) {
		server := setupTestServer(t)
		defer server.Close()

		resp, err := client.Get(server.URL+"/?foo=bar&baz=qux", nil)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, 200)
		}
	})

	t.Run("missing host", func(t *testing.T) {
		_, err := client.Get("http://", nil)
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if !errors.Is(err, client.ErrMissingHost) {
			t.Errorf("Get() error = %v, want %v", err, client.ErrMissingHost)
		}
	})

	t.Run("scheme only", func(t *testing.T) {
		_, err := client.Get("https://", nil)
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if !errors.Is(err, client.ErrMissingHost) {
			t.Errorf("Get() error = %v, want %v", err, client.ErrMissingHost)
		}
	})
}

// clientDialling returns an http.Client that trusts server's certificate and
// dials server for every address, so a request to any host reaches it.
func clientDialling(server *httptest.Server) *http.Client {
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, server.Listener.Addr().String())
	}
	return &http.Client{Transport: transport}
}
