package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type timeoutTool struct{ d time.Duration }

func (timeoutTool) Name() string                          { return "t" }
func (timeoutTool) Description() string                   { return "" }
func (timeoutTool) Parameters() json.RawMessage           { return nil }
func (timeoutTool) IsConcurrencySafe(map[string]any) bool { return false }
func (timeoutTool) IsReadOnly() bool                      { return true }
func (timeoutTool) ValidateInput(map[string]any) error    { return nil }
func (timeoutTool) InterruptBehavior() InterruptBehavior  { return InterruptCancel }
func (timeoutTool) Execute(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, nil
}
func (t timeoutTool) Timeout() time.Duration { return t.d }

func TestTimeoutForUsesToolTimeout(t *testing.T) {
	if got := TimeoutFor(timeoutTool{d: 5 * time.Minute}, 45*time.Second); got != 5*time.Minute {
		t.Fatalf("got %v, want 5m", got)
	}
}

func TestTimeoutForZeroMeansDefault(t *testing.T) {
	if got := TimeoutFor(timeoutTool{}, 45*time.Second); got != 45*time.Second {
		t.Fatalf("got %v, want the 45s default", got)
	}
}

type noTimeoutTool struct{}

func (noTimeoutTool) Name() string                          { return "n" }
func (noTimeoutTool) Description() string                   { return "" }
func (noTimeoutTool) Parameters() json.RawMessage           { return nil }
func (noTimeoutTool) IsConcurrencySafe(map[string]any) bool { return false }
func (noTimeoutTool) IsReadOnly() bool                      { return true }
func (noTimeoutTool) ValidateInput(map[string]any) error    { return nil }
func (noTimeoutTool) InterruptBehavior() InterruptBehavior  { return InterruptCancel }
func (noTimeoutTool) Execute(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, nil
}

func TestTimeoutForToolWithoutTimeouter(t *testing.T) {
	if got := TimeoutFor(noTimeoutTool{}, 30*time.Second); got != 30*time.Second {
		t.Fatalf("got %v, want the 30s default", got)
	}
}
