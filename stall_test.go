package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// These tests use real time, because a test bubble cannot fake time for
// network connections. The stall timeout is short, and every stalled server
// waits until the test ends, well past it.
const stallTimeout = 200 * time.Millisecond

// stallOption returns an Option with the stall timeout and a context deadline
// far beyond it, so a request that does not detect the stall fails instead of
// waiting for the server.
func stallOption(t *testing.T) *options.Option {
	ctx, cancel := context.WithTimeout(t.Context(), 25*stallTimeout)
	t.Cleanup(cancel)
	return options.New().SetContext(ctx).SetStallTimeout(stallTimeout)
}

// newStallServer returns a server that runs handler, and a channel that is
// closed when the test ends, so a handler that waits on it stops then.
func newStallServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, done <-chan struct{})) *httptest.Server {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, done)
	}))
	t.Cleanup(func() {
		close(done)
		server.Close()
	})
	return server
}

func TestStallTimeout(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request, done <-chan struct{})
		payload any
	}{
		{"download stops part way", func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("abc"))
			w.(http.Flusher).Flush()
			<-done
		}, nil},
		{"server never answers", func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
			<-done
		}, nil},
		{"server never reads the upload", func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
			<-done
		}, largefile.Bytes()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newStallServer(t, tt.handler)

			start := time.Now()
			resp, err := client.Post(server.URL, tt.payload, stallOption(t))
			if !errors.Is(err, client.ErrStalled) {
				t.Fatalf("Post() error = %v, want %v", err, client.ErrStalled)
			}
			if !errors.Is(resp.Error, client.ErrStalled) {
				t.Errorf("Response.Error = %v, want %v", resp.Error, client.ErrStalled)
			}
			if elapsed := time.Since(start); elapsed > 10*stallTimeout {
				t.Errorf("Post() returned after %v, want about %v", elapsed, stallTimeout)
			}
		})
	}
}

// TestStallTimeoutAllowsSteadyTransfer checks that a body that arrives in
// small parts, over a longer time than the timeout, is not cancelled.
func TestStallTimeoutAllowsSteadyTransfer(t *testing.T) {
	server := newStallServer(t, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		for range 10 {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(stallTimeout / 4)
		}
	})

	resp, err := client.Get(server.URL, stallOption(t))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got, want := resp.String(), "xxxxxxxxxx"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestStallTimeoutOffByDefault checks that, without a timeout, a request is
// not cancelled when the server sends no data for a while.
func TestStallTimeoutOffByDefault(t *testing.T) {
	server := newStallServer(t, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		time.Sleep(2 * stallTimeout)
		_, _ = w.Write([]byte("def"))
	})

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got, want := resp.String(), "abcdef"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestClientStallTimeout checks that a Client applies a global stall timeout,
// and that a per-request SetStallTimeout(0) removes it.
func TestClientStallTimeout(t *testing.T) {
	server := newStallServer(t, func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		time.Sleep(2 * stallTimeout)
		_, _ = w.Write([]byte("def"))
	})

	c := client.New(options.New().SetStallTimeout(stallTimeout))
	if _, err := c.Get(server.URL); !errors.Is(err, client.ErrStalled) {
		t.Errorf("Get() error = %v, want %v", err, client.ErrStalled)
	}

	resp, err := c.Get(server.URL, options.New().SetStallTimeout(0))
	if err != nil {
		t.Fatalf("Get() with SetStallTimeout(0) error = %v", err)
	}
	if got, want := resp.String(), "abcdef"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
