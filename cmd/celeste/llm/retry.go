package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type errKind int

const (
	kindNone errKind = iota
	kindRateLimit
	kindServer
	kindNetwork
	kindContextLength
	kindFatal
)

// ErrContextOverflow marks a request rejected because the conversation no
// longer fits the model's context window. Match it with errors.Is.
var ErrContextOverflow = errors.New("the conversation no longer fits the model's context window")

// contextOverflowMarkers are provider messages for a request that no longer
// fits the model's context window. Deliberately specific: rate-limit errors
// talk about tokens too ("tokens per minute"), and those must stay retryable.
var contextOverflowMarkers = []string{
	"context_length_exceeded",              // OpenAI-compatible error code
	"maximum context length",               // OpenAI-compatible message
	"maximum prompt length",                // xAI
	"prompt is too long",                   // Anthropic
	"exceeds the context window",           // generic
	"exceeds the maximum number of tokens", // Gemini
}

type errorClass struct {
	Retryable bool
	Kind      errKind
}

// ErrRuleInterrupt marks a stream a steering rule or the watchdog cut short
// (2.0 W3). withRetry never retries it: the loop re-runs the turn itself,
// with the rule's reminder.
var ErrRuleInterrupt = errors.New("stream interrupted by a steering rule")

// nonRetryable wraps an error so classifyError treats it as fatal regardless of
// message content (used when output already streamed and a retry would duplicate).
type nonRetryable struct{ err error }

func (n nonRetryable) Error() string { return n.err.Error() }
func (n nonRetryable) Unwrap() error { return n.err }
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
	// An error that knows its class (a Responses stream failure event)
	// says so; others are matched on their message below.
	var rk interface{ retryKind() (errKind, bool) }
	if errors.As(err, &rk) {
		if k, ok := rk.retryKind(); ok {
			return errorClass{Retryable: k == kindRateLimit || k == kindServer || k == kindNetwork, Kind: k}
		}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit"):
		return errorClass{Retryable: true, Kind: kindRateLimit}
	case containsAny(msg, contextOverflowMarkers):
		return errorClass{Retryable: false, Kind: kindContextLength}
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

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
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
	// gets exactly `timeout`. With stall set it is the hard cap
	// (MaxRequestDuration), not the user's timeout.
	timeout time.Duration
	// stall, when > 0, ends an attempt that receives nothing for this long
	// (ErrStalled): the config's `timeout`. A reply that keeps streaming is
	// never cut by it, so a slow local model can take as long as it needs,
	// while a dead connection still fails after one idle period.
	stall time.Duration
}

// withRetry runs fn, retrying transient errors per policy. Each attempt gets a
// fresh timeout-scoped context derived from base (when opts.timeout > 0) while
// still honoring base's cancellation (Ctrl+C). sleep is injected for tests.
func withRetry(base context.Context, opts retryOpts, fn func(ctx context.Context) error, sleep func(time.Duration)) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		ctx := base
		cancel := context.CancelFunc(func() {})
		if opts.timeout > 0 {
			ctx, cancel = context.WithTimeout(base, opts.timeout)
		}
		stopStall := func() {}
		if opts.stall > 0 {
			ctx, stopStall = withStall(ctx, opts.stall)
		}
		err := fn(ctx)
		// Capture before cancel(): once cancelled, ctx.Err() no longer tells us
		// whether the attempt ran out of time or was torn down.
		attemptStalled := opts.stall > 0 && errors.Is(context.Cause(ctx), ErrStalled) && base.Err() == nil
		attemptTimedOut := !attemptStalled && opts.timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded)
		stopStall()
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		// A steering rule cut the stream short: never replay it.
		if errors.Is(err, ErrRuleInterrupt) || errors.Is(context.Cause(base), ErrRuleInterrupt) {
			return fmt.Errorf("%w: %w", ErrRuleInterrupt, err)
		}

		// The caller cancelled (Ctrl+C / shutdown), not a transient failure —
		// stop rather than replaying the request against a dead parent.
		if base.Err() != nil {
			return lastErr
		}

		// Our OWN per-attempt deadline expired. That means the model needed more
		// time than we allowed — not a transient transport fault, even though the
		// error text ("context deadline exceeded") matches the network pattern in
		// classifyError. Retrying cannot help: each attempt gets the same
		// allowance and sends the same request, so every attempt dies at the
		// same wall.
		//
		// Issue #113: this burned 3x90s + backoff = 273s on sakana/fugu before
		// failing with a bare, uninformative error. Fail on the first attempt and
		// say which knob to turn.
		//
		// A stall is the same: the next attempt would wait just as long for a
		// model that is still busy (a local model prefilling) or a connection
		// that is gone.
		if attemptStalled {
			return fmt.Errorf("%w for %s (the stall timeout); a slow local model may need longer: raise it with `celeste config --set-timeout <seconds>`: %w", ErrStalled, opts.stall, err)
		}
		if attemptTimedOut && opts.stall > 0 {
			return fmt.Errorf("request still running after %s, the most one request may take; shorten the request: %w", opts.timeout, err)
		}
		if attemptTimedOut {
			return fmt.Errorf("request exceeded the %s per-request timeout; raise it with `celeste config --set-timeout <seconds>` or shorten the request: %w", opts.timeout, err)
		}
		cls := classifyError(err)
		// The history no longer fits the model's window. Callers that can
		// compact match ErrContextOverflow and retry once (#174); the message
		// says what the user can do if that isn't enough (#169).
		if cls.Kind == kindContextLength {
			return fmt.Errorf("%w. Run /context compact, or start a new session (/session new or /clear in the TUI). "+
				"For a local model, check that context_limit matches the server's window: %w", ErrContextOverflow, err)
		}
		if !cls.Retryable || attempt >= maxAttempts(cls) {
			return lastErr
		}
		sleep(backoffFor(cls, attempt))
	}
}
