// cmd/celeste/tools/executor_test.go
package tools

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newMockRegistry creates a Registry with the given mock tools pre-registered.
func newMockRegistry(tools ...Tool) *Registry {
	r := NewRegistry()
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

func TestStreamingToolExecutor_SingleTool(t *testing.T) {
	tool := &mockTool{name: "read_file", concurrencySafe: true, readOnly: true,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: "result from read_file"}, nil
		},
	}
	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "read_file", `{"path": "/tmp/test.txt"}`)
	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].CallID != "call_1" {
		t.Errorf("unexpected call ID: %q", results[0].CallID)
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
	if results[0].Result.Content != "result from read_file" {
		t.Errorf("unexpected content: %q", results[0].Result.Content)
	}
}

func TestStreamingToolExecutor_ConcurrentTools(t *testing.T) {
	var runningCount atomic.Int32
	var maxConcurrent atomic.Int32

	makeTool := func(name string) *mockTool {
		return &mockTool{
			name:            name,
			concurrencySafe: true,
			readOnly:        true,
			executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
				current := runningCount.Add(1)
				defer runningCount.Add(-1)
				// Track max concurrency
				for {
					old := maxConcurrent.Load()
					if current <= old || maxConcurrent.CompareAndSwap(old, current) {
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
				return ToolResult{Content: fmt.Sprintf("result from %s", name)}, nil
			},
		}
	}

	registry := newMockRegistry(makeTool("read_a"), makeTool("read_b"), makeTool("read_c"))
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "read_a", `{}`)
	exec.AddTool("call_2", "read_b", `{}`)
	exec.AddTool("call_3", "read_c", `{}`)
	exec.Done()
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify results are in original call order
	expectedOrder := []string{"call_1", "call_2", "call_3"}
	for i, expected := range expectedOrder {
		if results[i].CallID != expected {
			t.Errorf("result[%d].CallID = %q, want %q", i, results[i].CallID, expected)
		}
	}

	// Verify concurrency happened
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected at least 2 concurrent executions, got %d", maxConcurrent.Load())
	}
}

func TestStreamingToolExecutor_SerialTools(t *testing.T) {
	var executionOrder []string
	var mu sync.Mutex

	makeTool := func(name string) *mockTool {
		return &mockTool{
			name:            name,
			concurrencySafe: false, // NOT concurrency safe
			readOnly:        false,
			executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
				mu.Lock()
				executionOrder = append(executionOrder, name)
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return ToolResult{Content: fmt.Sprintf("result from %s", name)}, nil
			},
		}
	}

	registry := newMockRegistry(makeTool("write_a"), makeTool("write_b"), makeTool("write_c"))
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "write_a", `{}`)
	exec.AddTool("call_2", "write_b", `{}`)
	exec.AddTool("call_3", "write_c", `{}`)
	exec.Done()
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify serial execution order
	mu.Lock()
	defer mu.Unlock()
	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executionOrder))
	}
	if executionOrder[0] != "write_a" || executionOrder[1] != "write_b" || executionOrder[2] != "write_c" {
		t.Errorf("expected serial order [write_a, write_b, write_c], got %v", executionOrder)
	}

	// Results still in original call order
	for i, expected := range []string{"call_1", "call_2", "call_3"} {
		if results[i].CallID != expected {
			t.Errorf("result[%d].CallID = %q, want %q", i, results[i].CallID, expected)
		}
	}
}

func TestStreamingToolExecutor_MixedConcurrency(t *testing.T) {
	readTool := &mockTool{
		name: "read_file", concurrencySafe: true, readOnly: true,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return ToolResult{Content: "read result"}, nil
		},
	}
	writeTool := &mockTool{
		name: "write_file", concurrencySafe: false, readOnly: false,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return ToolResult{Content: "write result"}, nil
		},
	}

	registry := newMockRegistry(readTool, writeTool)
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "read_file", `{}`)
	exec.AddTool("call_2", "write_file", `{}`)
	exec.AddTool("call_3", "read_file", `{}`)
	exec.Done()
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All results present in order
	if results[0].CallID != "call_1" || results[1].CallID != "call_2" || results[2].CallID != "call_3" {
		t.Errorf("results out of order: %v, %v, %v", results[0].CallID, results[1].CallID, results[2].CallID)
	}

	// No errors
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", i, r.Err)
		}
	}
}

func TestStreamingToolExecutor_ToolNotFound(t *testing.T) {
	registry := newMockRegistry() // empty registry
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "nonexistent_tool", `{}`)
	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != ToolStateFailed {
		t.Errorf("expected Failed state, got %v", results[0].State)
	}
	if results[0].Err == nil {
		t.Error("expected error for missing tool")
	}
}

func TestStreamingToolExecutor_AddToolDuringExecution(t *testing.T) {
	// Verify tools can be added while others are running
	tool := &mockTool{
		name: "fast_tool", concurrencySafe: true, readOnly: true,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(10 * time.Millisecond)
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.AddTool("call_1", "fast_tool", `{}`)
	time.Sleep(5 * time.Millisecond) // Let first tool start
	exec.AddTool("call_2", "fast_tool", `{}`)

	// Signal no more tools will be added
	exec.Done()
	results := exec.Wait()

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestStreamingToolExecutor_EmptyWait(t *testing.T) {
	registry := newMockRegistry()
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	exec.Done()
	results := exec.Wait()

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestToolState_String(t *testing.T) {
	tests := []struct {
		state    ToolState
		expected string
	}{
		{ToolStateQueued, "Queued"},
		{ToolStateExecuting, "Executing"},
		{ToolStateCompleted, "Completed"},
		{ToolStateFailed, "Failed"},
		{ToolStateAborted, "Aborted"},
		{ToolState(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("ToolState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestStreamingToolExecutor_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tool := &mockTool{
		name: "interruptable", concurrencySafe: true, readOnly: true,
		interruptBehavior: InterruptCancel,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			select {
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return ToolResult{Content: "should not reach"}, nil
			}
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutorWithContext(ctx, registry)

	exec.AddTool("call_1", "interruptable", `{}`)

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should be failed or aborted due to context cancellation
	if results[0].State != ToolStateFailed && results[0].State != ToolStateAborted {
		t.Errorf("expected Failed or Aborted state, got %v", results[0].State)
	}
}

func TestStreamingToolExecutor_InterruptBehaviorBlock(t *testing.T) {
	var completed atomic.Bool

	tool := &mockTool{
		name: "blocking_tool", concurrencySafe: true, readOnly: true,
		interruptBehavior: InterruptBlock,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			// Simulate work that completes regardless of context
			time.Sleep(30 * time.Millisecond)
			completed.Store(true)
			return ToolResult{Content: "blocked"}, nil
		},
	}

	registry := newMockRegistry(tool)
	ctx, cancel := context.WithCancel(context.Background())
	exec := NewStreamingToolExecutorWithContext(ctx, registry)

	exec.AddTool("call_1", "blocking_tool", `{}`)

	// Cancel almost immediately - tool should still complete since it blocks
	time.Sleep(5 * time.Millisecond)
	cancel()

	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// A blocking tool should complete its current work
	if !completed.Load() {
		t.Error("blocking tool should have completed its work")
	}
}

func TestStreamingToolExecutor_AbortAllQueued(t *testing.T) {
	var executedCount atomic.Int32

	tool := &mockTool{
		name: "serial_tool", concurrencySafe: false, readOnly: false,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			executedCount.Add(1)
			time.Sleep(10 * time.Millisecond)
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutorWithContext(context.Background(), registry)

	// Add several serial tools
	exec.AddTool("call_1", "serial_tool", `{}`)
	exec.AddTool("call_2", "serial_tool", `{}`)
	exec.AddTool("call_3", "serial_tool", `{}`)

	// Cancel quickly
	time.Sleep(5 * time.Millisecond)
	exec.Cancel()

	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Not all 3 should have executed
	if executedCount.Load() == 3 {
		t.Error("expected cancellation to prevent some executions")
	}
}
