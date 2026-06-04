package llm

import (
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

// withRetry runs fn, retrying transient errors per policy. sleep is injected for tests.
func withRetry(fn func() error, sleep func(time.Duration)) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		cls := classifyError(err)
		if !cls.Retryable || attempt >= maxAttempts(cls) {
			return lastErr
		}
		sleep(backoffFor(cls, attempt))
	}
}
