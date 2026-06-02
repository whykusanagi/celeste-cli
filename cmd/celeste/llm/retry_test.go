package llm

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{"nil", nil, false},
		{"429", errors.New("status code 429: rate limit exceeded"), true},
		{"rate limit text", errors.New("Rate limit reached"), true},
		{"500", errors.New("status code 500"), true},
		{"503", errors.New("503 service unavailable"), true},
		{"conn reset", errors.New("read: connection reset by peer"), true},
		{"timeout", errors.New("context deadline exceeded"), true},
		{"eof", errors.New("unexpected EOF"), true},
		{"400", errors.New("status code 400: bad request"), false},
		{"401", errors.New("status code 401"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyError(c.err); got.Retryable != c.wantRetry {
				t.Fatalf("classifyError(%v).Retryable=%v want %v", c.err, got.Retryable, c.wantRetry)
			}
		})
	}
}

func TestMaxAttemptsAndBackoff(t *testing.T) {
	rl := classifyError(errors.New("429"))
	if maxAttempts(rl) != 3 {
		t.Fatalf("rate-limit max attempts = %d want 3", maxAttempts(rl))
	}
	if backoffFor(rl, 0) != 2*time.Second {
		t.Fatalf("rate-limit backoff[0] = %v want 2s", backoffFor(rl, 0))
	}
	srv := classifyError(errors.New("500"))
	if maxAttempts(srv) != 2 {
		t.Fatalf("server max attempts = %d want 2", maxAttempts(srv))
	}
	if backoffFor(srv, 0) != 1*time.Second {
		t.Fatalf("server backoff[0] = %v want 1s", backoffFor(srv, 0))
	}
}

func TestWithRetry_RetriesThenSucceeds(t *testing.T) {
	calls := 0
	err := withRetry(func() error {
		calls++
		if calls < 3 {
			return errors.New("status code 429")
		}
		return nil
	}, func(time.Duration) {})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d want nil,3", err, calls)
	}
}

func TestWithRetry_FatalFailsFast(t *testing.T) {
	calls := 0
	_ = withRetry(func() error { calls++; return errors.New("status code 400") }, func(time.Duration) {})
	if calls != 1 {
		t.Fatalf("400 should fail fast, calls=%d want 1", calls)
	}
}

func TestWithRetry_ServerGivesUpAfterMax(t *testing.T) {
	calls := 0
	_ = withRetry(func() error { calls++; return errors.New("status code 503") }, func(time.Duration) {})
	if calls != 3 { // initial + 2 retries
		t.Fatalf("server calls=%d want 3", calls)
	}
}

func TestNonRetryableWrapper(t *testing.T) {
	if classifyError(fatalErr(errors.New("status code 503"))).Retryable {
		t.Fatal("wrapped fatalErr must be non-retryable even if message looks transient")
	}
}
