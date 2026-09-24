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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), conns.Load(), "sequential package-level requests should share one connection")
}

func TestClientPerRequestOptionsReuseConnections(t *testing.T) {
	server, conns := newCountingServer(t)

	c := client.New()
	for range 3 {
		_, err := c.Get(server.URL, options.New().AddHeader("X-Request", "1"))
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), conns.Load(), "per-request options should not create a new connection pool")
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
	require.NoError(t, err)

	_, err = c.Get(server.URL, options.New().SetTransport(transport))
	require.NoError(t, err)

	assert.Equal(t, int32(1), dials.Load(), "the per-request transport should carry the request")
}

func TestClientTransportOverrideDoesNotPersist(t *testing.T) {
	server, _ := newCountingServer(t)

	c := client.New()
	_, err := c.Get(server.URL, options.New().SetMaxResponseHeaderBytes(512))
	require.Error(t, err, "the 4 KB response header should exceed the 512 byte limit")

	_, err = c.Get(server.URL)
	assert.NoError(t, err, "the header limit should not apply to later requests")
}

func TestPerRequestClientHasNoTimeout(t *testing.T) {
	opt := options.New().UsePerRequestClient()

	assert.Zero(t, opt.Client().Timeout, "a total timeout would cut off long downloads; use a context deadline instead")
}
