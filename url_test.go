package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

	// A host with a port and no scheme, such as localhost:8080, gets the scheme
	// too, although net/url alone reads its host as a scheme.
	t.Run("no scheme with port uses https", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()

		c := client.NewCustom(server.Client())
		resp, err := c.Get(strings.TrimPrefix(server.URL, "https://"))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("no scheme with port uses set scheme", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()
		port := strings.TrimPrefix(server.URL, "http://127.0.0.1")

		for _, host := range []string{"127.0.0.1", "localhost"} {
			resp, err := client.Get(host+port, options.New().SetProtocolScheme("http"))
			if err != nil {
				t.Fatalf("Get(%q) error = %v", host+port, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Get(%q) StatusCode = %d, want %d", host+port, resp.StatusCode, http.StatusOK)
			}
		}
	})

	// A URL in the path, query or fragment is part of the address, not its
	// scheme, so the address still gets the scheme.
	t.Run("no scheme with embedded url uses set scheme", func(t *testing.T) {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.RequestURI()
		}))
		defer server.Close()
		host := strings.TrimPrefix(server.URL, "http://")

		tests := []struct {
			name string
			path string
			want string
		}{
			{"query", "/fetch?next=https://example.test/file", "/fetch?next=https://example.test/file"},
			{"path", "/fetch/https://example.test/file", "/fetch/https://example.test/file"},
			{"fragment", "/fetch#https://example.test/file", "/fetch"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp, err := client.Get(host+tt.path, options.New().SetProtocolScheme("http"))
				if err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if got != tt.want {
					t.Errorf("server received %q, want %q", got, tt.want)
				}
			})
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
