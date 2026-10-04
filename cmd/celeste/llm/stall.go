package llm

import (
	"context"
	"errors"
	"fmt"
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

// errNoFirstByte is the cause of an attempt that received no reply data
// for the whole first-byte budget. It wraps ErrStalled.
var errNoFirstByte = fmt.Errorf("%w while waiting for the first byte", ErrStalled)

// stallWatch cancels its context when no activity is recorded for idle.
// Activity is any byte of the response body (stallTransport) or any stream
// callback, so a model sending reasoning that celeste never shows still
// counts as alive. Until the first reply data arrives the allowance is
// first instead (#359): a local model prefilling a long prompt sends
// nothing for minutes, and response headers alone do not end that wait.
type stallWatch struct {
	idle    time.Duration
	first   time.Duration
	last    atomic.Int64 // UnixNano of the last activity
	started atomic.Bool  // reply data has arrived
	cancel  context.CancelCauseFunc

	mu    sync.Mutex
	timer *time.Timer
	done  bool
}

type stallKey struct{}

// withStall derives a context that is cancelled with ErrStalled after idle
// without activity, or after first while no reply data has arrived yet
// (first no longer than idle means idle throughout). The returned watch
// reports whether data arrived; stop releases the timer.
func withStall(parent context.Context, idle, first time.Duration) (ctx context.Context, w *stallWatch, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	w = &stallWatch{idle: idle, first: max(first, idle), cancel: cancel}
	w.touch()
	w.mu.Lock()
	w.timer = time.AfterFunc(w.first, w.check)
	w.mu.Unlock()
	return context.WithValue(ctx, stallKey{}, w), w, func() {
		w.mu.Lock()
		w.done = true
		w.timer.Stop()
		w.mu.Unlock()
		cancel(context.Canceled)
	}
}

func (w *stallWatch) touch() { w.last.Store(time.Now().UnixNano()) }

// data records reply data. The first call ends the first-byte wait: the
// timer re-arms for the stall timeout, which applies from then on.
func (w *stallWatch) data() {
	w.touch()
	if w.started.Load() || !w.started.CompareAndSwap(false, true) || w.first == w.idle {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.done {
		w.timer.Reset(w.idle)
	}
}

// check runs when the timer fires: a full idle period without activity
// cancels; otherwise the timer re-arms for what is left of the period.
func (w *stallWatch) check() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	idle, cause := w.idle, ErrStalled
	if !w.started.Load() {
		idle, cause = w.first, errNoFirstByte
	}
	since := time.Since(time.Unix(0, w.last.Load()))
	if since >= idle {
		w.done = true
		w.cancel(cause)
		return
	}
	w.timer.Reset(idle - since)
}

// touchStall records reply data on ctx's stall watch, if it has one.
func touchStall(ctx context.Context) {
	if w, ok := ctx.Value(stallKey{}).(*stallWatch); ok {
		w.data()
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
		b.w.data()
	}
	return n, err
}

// newHTTPClient is the HTTP client every backend that accepts one uses. It
// has no overall timeout: the request context carries the stall timeout and
// the hard cap (withRetry).
func newHTTPClient() *http.Client {
	return &http.Client{Transport: stallTransport{}}
}
