package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// stallWatch cancels a request's context with ErrStalled as the cause when no
// data is sent or received for longer than its timeout.
type stallWatch struct {
	ctx     context.Context
	cancel  context.CancelCauseFunc
	timer   *time.Timer
	timeout time.Duration
}

// watchStall returns a context derived from parent, and the stallWatch that
// cancels it. The time starts at once.
func watchStall(parent context.Context, timeout time.Duration) (context.Context, *stallWatch) {
	ctx, cancel := context.WithCancelCause(parent)
	w := &stallWatch{ctx: ctx, cancel: cancel, timeout: timeout}
	w.timer = time.AfterFunc(timeout, func() { cancel(ErrStalled) })
	return ctx, w
}

// restart starts the time again.
func (w *stallWatch) restart() {
	w.timer.Reset(w.timeout)
}

// stalled reports whether the watch cancelled the context.
func (w *stallWatch) stalled() bool {
	return errors.Is(context.Cause(w.ctx), ErrStalled)
}

// stop stops the time and releases the context.
func (w *stallWatch) stop() {
	w.timer.Stop()
	w.cancel(nil)
}

// stallTransport restarts a stallWatch each time data is sent or received. It
// wraps the transport of one request, so it also sees the data of every
// redirect.
type stallTransport struct {
	next  http.RoundTripper
	watch *stallWatch
}

// RoundTrip sends req through the next transport with both bodies watched.
func (t *stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody {
		// A RoundTripper must not change the request it is given.
		req = req.Clone(req.Context())
		req.Body = &stallBody{ReadCloser: req.Body, watch: t.watch}
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &stallBody{ReadCloser: resp.Body, watch: t.watch}
	return resp, nil
}

// stallBody restarts a stallWatch each time a read returns data.
type stallBody struct {
	io.ReadCloser
	watch *stallWatch
}

// Read reads from the body and restarts the watch when it returns data.
func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.watch.restart()
	}
	return n, err
}
