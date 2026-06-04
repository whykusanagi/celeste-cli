package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
)

// A tool that completes well within the deadline returns its result unchanged.
func TestRunToolWithTimeout_FastToolReturnsResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	want := &llm.ExecutionResult{}
	got, err := runToolWithTimeout(ctx, func() (*llm.ExecutionResult, error) {
		return want, nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != want {
		t.Fatalf("expected the tool's own result to pass through")
	}
}

// The tool's own error is propagated unchanged when it finishes in time.
func TestRunToolWithTimeout_FastToolError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	sentinel := errors.New("tool failed")
	_, err := runToolWithTimeout(ctx, func() (*llm.ExecutionResult, error) {
		return nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the tool's own error to propagate, got %v", err)
	}
}

// This is the regression for the v1.10 uncancellable-subagent hang (task 349f1f14):
// a tool that ignores its context must NOT block the caller past the deadline.
// runToolWithTimeout must return promptly with a deadline error even though the
// tool goroutine keeps running.
func TestRunToolWithTimeout_UncooperativeToolDoesNotBlockPastDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	released := make(chan struct{})
	defer close(released) // let the abandoned goroutine exit when the test ends

	start := time.Now()
	_, err := runToolWithTimeout(ctx, func() (*llm.ExecutionResult, error) {
		// Simulate a tool stuck in a loop that never checks ctx.
		<-released
		return &llm.ExecutionResult{}, nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a deadline error from an uncooperative tool")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runToolWithTimeout blocked %v — it must return at the deadline, not wait for the tool", elapsed)
	}
}
