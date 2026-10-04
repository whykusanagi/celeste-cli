package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStalled marks a request that received nothing from the provider for the
// whole stall timeout (the config's `timeout`). Match it with errors.Is.
var ErrStalled = errors.New("no data from the model")

// MaxRequestDuration bounds one request however steadily it streams: 30
// minutes, or three times the stall timeout when that is longer. The stall
// timeout catches a dead connection; this catches a reply that never ends.
func MaxRequestDuration(stall time.Duration) time.Duration {
	return max(30*time.Minute, 3*stall)
}

// stallWatch cancels its context when no activity is recorded for idle.
// Activity is any byte of the response body (stallTransport) or any stream
// callback, so a model sending reasoning that celeste never shows still
// counts as alive.
type stallWatch struct {
	idle   time.Duration
	last   atomic.Int64 // UnixNano of the last activity
	cancel context.CancelCauseFunc

	mu    sync.Mutex
	timer *time.Timer
	done  bool
}

type stallKey struct{}

// withStall derives a context that is cancelled with ErrStalled after idle
// without activity. stop releases the timer.
func withStall(parent context.Context, idle time.Duration) (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	w := &stallWatch{idle: idle, cancel: cancel}
	w.touch()
	w.mu.Lock()
	w.timer = time.AfterFunc(idle, w.check)
	w.mu.Unlock()
	return context.WithValue(ctx, stallKey{}, w), func() {
		w.mu.Lock()
		w.done = true
		w.timer.Stop()
		w.mu.Unlock()
		cancel(context.Canceled)
	}
}

func (w *stallWatch) touch() { w.last.Store(time.Now().UnixNano()) }

// check runs when the timer fires: a full idle period without activity
// cancels; otherwise the timer re-arms for what is left of the period.
func (w *stallWatch) check() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	since := time.Since(time.Unix(0, w.last.Load()))
	if since >= w.idle {
		w.done = true
		w.cancel(ErrStalled)
		return
	}
	w.timer.Reset(w.idle - since)
}

// touchStall records activity on ctx's stall watch, if it has one.
func touchStall(ctx context.Context) {
	if w, ok := ctx.Value(stallKey{}).(*stallWatch); ok {
		w.touch()
	}
}

// stallTransport records every response byte as activity on the request
// context's stall watch. Requests without one pass through untouched.
type stallTransport struct{ base http.RoundTripper }

func (t stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if w, ok := req.Context().Value(stallKey{}).(*stallWatch); ok {
		w.touch() // the headers arrived
		resp.Body = &stallBody{ReadCloser: resp.Body, w: w}
	}
	return resp, nil
}

type stallBody struct {
	io.ReadCloser
	w *stallWatch
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.w.touch()
	}
	return n, err
}

// newHTTPClient is the HTTP client every backend that accepts one uses. It
// has no overall timeout: the request context carries the stall timeout and
// the hard cap (withRetry).
func newHTTPClient() *http.Client {
	return &http.Client{Transport: stallTransport{}}
}
