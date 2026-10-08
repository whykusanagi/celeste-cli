package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The permission request carries the whole command the tool will run, so
// what the user approves is what runs, and a field the model added that
// the tool never reads is not what the prompt leads with.
func TestPermissionRequestCarriesTheFullCommand(t *testing.T) {
	var got PermissionRequest
	r := NewRegistry()
	r.Register(&mockTool{
		name:   "bash",
		params: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
	})
	r.SetPermissionChecker(newAskChecker())
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		got = req
		return PermissionResponse{Decision: "deny"}
	})
	cmd := "echo " + strings.Repeat("a", 120) + " && echo tail-of-the-command"
	if _, err := r.Execute(context.Background(), "bash", map[string]any{"command": cmd}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.InputSummary, cmd) {
		t.Fatalf("summary %q does not carry the full command", got.InputSummary)
	}

	r.Register(&mockTool{
		name:   "write_file",
		params: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`),
	})
	if _, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "real/target.txt", "content": "x", "command": "notes.txt"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.InputSummary, "real/target.txt") {
		t.Fatalf("summary %q does not lead with the path the tool writes", got.InputSummary)
	}
	if !strings.Contains(got.InputSummary, "command") {
		t.Fatalf("summary %q hides the extra field", got.InputSummary)
	}

	// A very large value is cut with an explicit marker, never silently.
	huge := strings.Repeat("b", maxSummaryValue+500)
	if _, err := r.Execute(context.Background(), "bash", map[string]any{"command": huge}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.InputSummary, "[500 more chars]") {
		t.Fatalf("a cut summary does not say how much is hidden: ...%q", got.InputSummary[len(got.InputSummary)-40:])
	}
}
