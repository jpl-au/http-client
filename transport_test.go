package client_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// newCountingServer returns a test server and the number of TCP connections it has accepted.
func newCountingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var conns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Large", strings.Repeat("a", 4096))
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return server, &conns
}

func TestPackageFunctionsReuseConnections(t *testing.T) {
	server, conns := newCountingServer(t)

	for range 3 {
		_, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
	}

	if got := conns.Load(); got != 1 {
		t.Errorf("connections = %d, want 1 (sequential package-level requests should share one connection)", got)
	}
}

func TestClientPerRequestOptionsReuseConnections(t *testing.T) {
	server, conns := newCountingServer(t)

	c := client.New()
	for range 3 {
		_, err := c.Get(server.URL, options.New().AddHeader("X-Request", "1"))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
	}

	if got := conns.Load(); got != 1 {
		t.Errorf("connections = %d, want 1 (per-request options should not create a new connection pool)", got)
	}
}

func TestClientPerRequestTransport(t *testing.T) {
	server, _ := newCountingServer(t)

	var dials atomic.Int32
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials.Add(1)
			return dialer.DialContext(ctx, network, addr)
		},
	}
	defer transport.CloseIdleConnections()

	c := client.New()
	_, err := c.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	_, err = c.Get(server.URL, options.New().SetTransport(transport))
	if err != nil {
		t.Fatalf("Get() with SetTransport error = %v", err)
	}

	if got := dials.Load(); got != 1 {
		t.Errorf("per-request transport dials = %d, want 1 (the per-request transport should carry the request)", got)
	}
}

func TestClientTransportOverrideDoesNotPersist(t *testing.T) {
	server, _ := newCountingServer(t)

	c := client.New()
	_, err := c.Get(server.URL, options.New().SetMaxResponseHeaderBytes(512))
	if err == nil {
		t.Fatal("Get() with a 512 byte header limit error = nil, want error for the 4 KB response header")
	}

	_, err = c.Get(server.URL)
	if err != nil {
		t.Errorf("Get() after the header limit request error = %v, want nil (the header limit should not apply to later requests)", err)
	}
}

func TestPerRequestClientHasNoTimeout(t *testing.T) {
	opt := options.New().UsePerRequestClient()

	if got := opt.Client().Timeout; got != 0 {
		t.Errorf("Client().Timeout = %v, want 0 (a total timeout would cut off long downloads; use a context deadline instead)", got)
	}
}

func TestSetProtocolScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"h", "h://"},
		{"ws", "ws://"},
		{"http", "http://"},
		{"https://", "https://"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			opt := options.New().SetProtocolScheme(tt.input)
			if got := opt.Transport.Scheme; got != tt.want {
				t.Errorf("SetProtocolScheme(%q): Transport.Scheme = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
