// Package tools provides the unified tool abstraction for Celeste CLI.
// This file implements StreamingToolExecutor, a state machine that accepts
// tool calls as they arrive during LLM streaming and dispatches them
// according to their concurrency safety.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ToolState represents the lifecycle state of a tool execution.
type ToolState int

const (
	// ToolStateQueued means the tool is waiting to be executed.
	ToolStateQueued ToolState = iota
	// ToolStateExecuting means the tool is currently running.
	ToolStateExecuting
	// ToolStateCompleted means the tool finished successfully.
	ToolStateCompleted
	// ToolStateFailed means the tool finished with an error.
	ToolStateFailed
	// ToolStateAborted means the tool was cancelled (cascading failure or interrupt).
	ToolStateAborted
)

// String returns a human-readable name for the tool state.
func (s ToolState) String() string {
	switch s {
	case ToolStateQueued:
		return "Queued"
	case ToolStateExecuting:
		return "Executing"
	case ToolStateCompleted:
		return "Completed"
	case ToolStateFailed:
		return "Failed"
	case ToolStateAborted:
		return "Aborted"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// ExecutorResult holds the result of a single tool execution,
// including the call metadata and final state.
type ExecutorResult struct {
	CallID   string     // The tool call ID from the LLM
	ToolName string     // The tool name
	State    ToolState  // Final state
	Result   ToolResult // Tool output (content and/or error)
	Err      error      // Go error if tool execution returned one
}

// toolEntry tracks a single tool call through its lifecycle.
type toolEntry struct {
	callID     string
	toolName   string
	inputJSON  string
	state      ToolState
	result     ToolResult
	err        error
	index      int  // original insertion order
	concurrent bool // whether this tool can run concurrently
}

// StreamingToolExecutor accepts tool calls as they arrive during LLM
// streaming and dispatches them for execution. Concurrency-safe tools
// run in parallel goroutines; non-concurrent tools are queued and
// executed serially. Results are buffered and returned in original
// call order when Wait() is called.
type StreamingToolExecutor struct {
	registry *Registry
	ctx      context.Context
	cancel   context.CancelFunc

	mu          sync.Mutex
	entries     []*toolEntry // all entries in insertion order
	entryByID   map[string]*toolEntry
	serialQueue chan *toolEntry // serial tools are fed through this channel
	wg          sync.WaitGroup  // tracks all executing goroutines
	done        chan struct{}   // closed when Done() is called (no more tools)
	doneOnce    sync.Once

	progressFn func(ProgressEvent)
	progressMu sync.RWMutex

	cascadeOnFailure bool // when true, a failed tool cancels all siblings
}

// NewStreamingToolExecutor creates a new executor bound to the given registry.
// The executor uses a background context; cancel it to abort all running tools.
func NewStreamingToolExecutor(registry *Registry) *StreamingToolExecutor {
	ctx, cancel := context.WithCancel(context.Background())
	e := &StreamingToolExecutor{
		registry:    registry,
		ctx:         ctx,
		cancel:      cancel,
		entryByID:   make(map[string]*toolEntry),
		serialQueue: make(chan *toolEntry, 256),
		done:        make(chan struct{}),
	}
	// Start the serial queue consumer
	e.wg.Add(1)
	go e.serialWorker()
	return e
}

// NewStreamingToolExecutorWithContext creates a new executor with a parent context.
// Cancelling the parent context will abort all running tools.
func NewStreamingToolExecutorWithContext(ctx context.Context, registry *Registry) *StreamingToolExecutor {
	childCtx, cancel := context.WithCancel(ctx)
	e := &StreamingToolExecutor{
		registry:    registry,
		ctx:         childCtx,
		cancel:      cancel,
		entryByID:   make(map[string]*toolEntry),
		serialQueue: make(chan *toolEntry, 256),
		done:        make(chan struct{}),
	}
	e.wg.Add(1)
	go e.serialWorker()
	return e
}

// OnProgress registers a callback for tool progress events.
// Must be called before AddTool. Not safe to call concurrently with AddTool.
func (e *StreamingToolExecutor) OnProgress(fn func(ProgressEvent)) {
	e.progressMu.Lock()
	defer e.progressMu.Unlock()
	e.progressFn = fn
}

// SetCascadeOnFailure enables cascading failure mode.
// When enabled, if any tool fails, all queued and executing sibling
// tools are cancelled via context cancellation.
func (e *StreamingToolExecutor) SetCascadeOnFailure(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cascadeOnFailure = enabled
}

// AddTool submits a tool call for execution. This method is non-blocking.
// It parses the input JSON, looks up the tool in the registry, determines
// concurrency safety, and either launches a goroutine or enqueues for serial
// execution. If the tool is not found, the entry is immediately marked Failed.
//
// inputJSON is the raw JSON arguments string from the LLM.
func (e *StreamingToolExecutor) AddTool(callID, toolName, inputJSON string) {
	// Parse input JSON
	var input map[string]any
	if inputJSON != "" && inputJSON != "{}" {
		if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
			// If JSON parsing fails, store the error
			entry := &toolEntry{
				callID:   callID,
				toolName: toolName,
				state:    ToolStateFailed,
				result:   ToolResult{Content: fmt.Sprintf("failed to parse tool input JSON: %v", err), Error: true},
				err:      fmt.Errorf("failed to parse tool input JSON: %w", err),
			}
			e.mu.Lock()
			entry.index = len(e.entries)
			e.entries = append(e.entries, entry)
			e.entryByID[callID] = entry
			e.mu.Unlock()
			return
		}
	}
	if input == nil {
		input = make(map[string]any)
	}

	// Look up tool in registry
	tool, ok := e.registry.Get(toolName)
	if !ok {
		entry := &toolEntry{
			callID:   callID,
			toolName: toolName,
			state:    ToolStateFailed,
			result:   ToolResult{Content: fmt.Sprintf("tool not found: %s", toolName), Error: true},
			err:      fmt.Errorf("tool not found: %s", toolName),
		}
		e.mu.Lock()
		entry.index = len(e.entries)
		e.entries = append(e.entries, entry)
		e.entryByID[callID] = entry
		e.mu.Unlock()
		return
	}

	// Determine concurrency safety
	concurrent := tool.IsConcurrencySafe(input)

	entry := &toolEntry{
		callID:     callID,
		toolName:   toolName,
		inputJSON:  inputJSON,
		state:      ToolStateQueued,
		concurrent: concurrent,
	}

	e.mu.Lock()
	entry.index = len(e.entries)
	e.entries = append(e.entries, entry)
	e.entryByID[callID] = entry
	e.mu.Unlock()

	if concurrent {
		// Launch in a goroutine immediately
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.executeTool(entry, tool, input)
		}()
	} else {
		// Enqueue for serial execution
		e.serialQueue <- entry
	}
}

// Done signals that no more tool calls will be added.
// Must be called before Wait() will return.
func (e *StreamingToolExecutor) Done() {
	e.doneOnce.Do(func() {
		close(e.done)
	})
}

// Wait blocks until all tool calls have completed and returns results
// in the original call order. Callers must call Done() before or concurrently
// with Wait(), otherwise Wait() will block forever.
func (e *StreamingToolExecutor) Wait() []ExecutorResult {
	// Wait for Done() signal, then close serial queue
	<-e.done
	close(e.serialQueue)

	// Wait for all goroutines to finish
	e.wg.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]ExecutorResult, len(e.entries))
	for i, entry := range e.entries {
		results[i] = ExecutorResult{
			CallID:   entry.callID,
			ToolName: entry.toolName,
			State:    entry.state,
			Result:   entry.result,
			Err:      entry.err,
		}
	}
	return results
}

// States returns a snapshot of current tool states.
func (e *StreamingToolExecutor) States() map[string]ToolState {
	e.mu.Lock()
	defer e.mu.Unlock()
	states := make(map[string]ToolState, len(e.entries))
	for _, entry := range e.entries {
		states[entry.callID] = entry.state
	}
	return states
}

// Cancel aborts all running and queued tools.
func (e *StreamingToolExecutor) Cancel() {
	e.cancel()
	e.Done() // ensure Wait() can return
}

// serialWorker processes the serial queue, executing one tool at a time.
func (e *StreamingToolExecutor) serialWorker() {
	defer e.wg.Done()

	for entry := range e.serialQueue {
		// Check if we've been cancelled
		if e.ctx.Err() != nil {
			e.mu.Lock()
			entry.state = ToolStateAborted
			entry.result = ToolResult{Content: "aborted: context cancelled", Error: true}
			entry.err = e.ctx.Err()
			e.mu.Unlock()
			continue
		}

		// Look up tool again (should always succeed since AddTool checked)
		tool, ok := e.registry.Get(entry.toolName)
		if !ok {
			e.mu.Lock()
			entry.state = ToolStateFailed
			entry.result = ToolResult{Content: fmt.Sprintf("tool not found: %s", entry.toolName), Error: true}
			entry.err = fmt.Errorf("tool not found: %s", entry.toolName)
			e.mu.Unlock()
			continue
		}

		var input map[string]any
		if entry.inputJSON != "" && entry.inputJSON != "{}" {
			_ = json.Unmarshal([]byte(entry.inputJSON), &input)
		}
		if input == nil {
			input = make(map[string]any)
		}

		e.executeTool(entry, tool, input)
	}
}

// executeTool runs a single tool and updates the entry state.
func (e *StreamingToolExecutor) executeTool(entry *toolEntry, tool Tool, input map[string]any) {
	// Transition to Executing
	e.mu.Lock()
	if entry.state == ToolStateAborted {
		e.mu.Unlock()
		return
	}
	// Check if context is already cancelled (unless tool blocks on interrupt)
	if e.ctx.Err() != nil && tool.InterruptBehavior() != InterruptBlock {
		entry.state = ToolStateAborted
		entry.result = ToolResult{Content: "aborted: context cancelled", Error: true}
		entry.err = e.ctx.Err()
		e.mu.Unlock()
		return
	}
	entry.state = ToolStateExecuting
	e.mu.Unlock()

	// Set up progress channel
	var progressCh chan ProgressEvent
	e.progressMu.RLock()
	hasFn := e.progressFn != nil
	e.progressMu.RUnlock()

	var progressDone chan struct{}
	if hasFn {
		progressCh = make(chan ProgressEvent, 16)
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			for event := range progressCh {
				e.progressMu.RLock()
				fn := e.progressFn
				e.progressMu.RUnlock()
				if fn != nil {
					fn(event)
				}
			}
		}()
	}

	// For tools with InterruptBlock behavior, create an independent context
	// that ignores parent cancellation, so the tool can finish its current work.
	execCtx := e.ctx
	if tool.InterruptBehavior() == InterruptBlock {
		// Block tools get a detached context with a generous timeout
		// so they can complete current work even if parent is cancelled.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		execCtx = drainCtx
	}

	// Execute the tool
	result, err := tool.Execute(execCtx, input, progressCh)

	// Close progress channel and wait for forwarding goroutine to drain
	if progressCh != nil {
		close(progressCh)
		<-progressDone
	}

	// Update state
	e.mu.Lock()
	if entry.state == ToolStateAborted {
		// Was aborted during execution
		e.mu.Unlock()
		return
	}
	if err != nil {
		entry.state = ToolStateFailed
		entry.err = err
		if result.Content == "" {
			result.Content = err.Error()
		}
		result.Error = true
	} else if result.Error {
		entry.state = ToolStateFailed
		entry.err = fmt.Errorf("%s", result.Content)
	} else {
		entry.state = ToolStateCompleted
	}
	entry.result = result

	// Cascade: if this tool failed and cascade mode is on, cancel everything
	shouldCascade := entry.state == ToolStateFailed && e.cascadeOnFailure
	e.mu.Unlock()

	if shouldCascade {
		// Cancel all siblings
		e.cancel()

		// Mark all queued entries as aborted
		e.mu.Lock()
		for _, other := range e.entries {
			if other.callID != entry.callID && other.state == ToolStateQueued {
				other.state = ToolStateAborted
				other.result = ToolResult{
					Content: fmt.Sprintf("aborted: sibling tool %q failed", entry.toolName),
					Error:   true,
				}
				other.err = fmt.Errorf("aborted: sibling tool %q failed", entry.toolName)
			}
		}
		e.mu.Unlock()
	}
}
