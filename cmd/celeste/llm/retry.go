package llm

import (
	"context"
	"strings"
	"time"
)

type errKind int

const (
	kindNone errKind = iota
	kindRateLimit
	kindServer
	kindNetwork
	kindFatal
)

type errorClass struct {
	Retryable bool
	Kind      errKind
}

// nonRetryable wraps an error so classifyError treats it as fatal regardless of
// message content (used when output already streamed and a retry would duplicate).
type nonRetryable struct{ err error }

func (n nonRetryable) Error() string { return n.err.Error() }
func fatalErr(err error) error       { return nonRetryable{err} }

// classifyError inspects an error and decides retry policy. SDK errors are opaque
// so we match on message content. 429 -> rate limit; 5xx -> server; conn reset /
// timeout / EOF -> network; other 4xx -> fatal.
func classifyError(err error) errorClass {
	if err == nil {
		return errorClass{}
	}
	if _, ok := err.(nonRetryable); ok {
		return errorClass{Retryable: false, Kind: kindFatal}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit"):
		return errorClass{Retryable: true, Kind: kindRateLimit}
	case strings.Contains(msg, "500") || strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") || strings.Contains(msg, "504"):
		return errorClass{Retryable: true, Kind: kindServer}
	case strings.Contains(msg, "connection reset") || strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "eof"):
		return errorClass{Retryable: true, Kind: kindNetwork}
	default:
		return errorClass{Retryable: false, Kind: kindFatal}
	}
}

func maxAttempts(c errorClass) int {
	switch c.Kind {
	case kindRateLimit:
		return 3
	case kindServer, kindNetwork:
		return 2
	default:
		return 0
	}
}

// backoffFor returns the wait before retry attempt n (0-indexed).
func backoffFor(c errorClass, attempt int) time.Duration {
	switch c.Kind {
	case kindRateLimit: // 2s, 4s, 8s
		return time.Duration(2<<attempt) * time.Second
	case kindServer, kindNetwork: // 1s, 2s, 4s
		return time.Duration(1<<attempt) * time.Second
	default:
		return 0
	}
}

// retryOpts configures per-attempt behavior for withRetry.
type retryOpts struct {
	// timeout, when > 0, gives EACH attempt a fresh deadline derived from the base
	// context. This is the fix for the doomed-retry bug: baking a single deadline
	// into the base ctx meant a timeout on attempt 1 left an already-expired ctx,
	// so the retry failed instantly. A fresh per-attempt deadline lets the retry
	// actually run. It does NOT raise the configured timeout — each attempt still
	// gets exactly `timeout`.
	timeout time.Duration
	// beforeTry runs before each attempt (attempt is 0-based). Callers use it to
	// trim the outbound payload — progressively more on later attempts — so a
	// retry doesn't replay an identical oversized request.
	beforeTry func(attempt int)
}

// withRetry runs fn, retrying transient errors per policy. Each attempt gets a
// fresh timeout-scoped context derived from base (when opts.timeout > 0) while
// still honoring base's cancellation (Ctrl+C). sleep is injected for tests.
func withRetry(base context.Context, opts retryOpts, fn func(ctx context.Context) error, sleep func(time.Duration)) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if opts.beforeTry != nil {
			opts.beforeTry(attempt)
		}

		ctx := base
		cancel := context.CancelFunc(func() {})
		if opts.timeout > 0 {
			ctx, cancel = context.WithTimeout(base, opts.timeout)
		}
		err := fn(ctx)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		// The caller cancelled (Ctrl+C / shutdown), not a transient failure —
		// stop rather than replaying the request against a dead parent.
		if base.Err() != nil {
			return lastErr
		}
		cls := classifyError(err)
		if !cls.Retryable || attempt >= maxAttempts(cls) {
			return lastErr
		}
		sleep(backoffFor(cls, attempt))
	}
}
