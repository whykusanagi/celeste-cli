package subagents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func findings(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = map[string]any{"title": "t"}
	}
	return out
}

// Review Focus 2: an invalid submission returns errors the model can act
// on, naming the field, and stores nothing.
func TestSubmitResultValidates(t *testing.T) {
	h := &resultHolder{}
	tool := NewSubmitResultTool(h)
	if !tool.IsReadOnly() || tool.Name() != "submit_result" {
		t.Fatal("submit_result must be a read-only tool named submit_result")
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil || schema["additionalProperties"] != false {
		t.Fatalf("schema = %v %v", schema, err)
	}

	bad := []struct {
		name  string
		in    map[string]any
		field string
	}{
		{"missing summary", map[string]any{"findings": []any{}}, "summary"},
		{"empty summary", map[string]any{"summary": "  "}, "summary"},
		{"long summary", map[string]any{"summary": strings.Repeat("x", 4001)}, "summary"},
		{"string findings", map[string]any{"summary": "s", "findings": "two bugs"}, "findings"},
		{"finding not an object", map[string]any{"summary": "s", "findings": []any{"bug"}}, "findings[0]"},
		{"finding without title", map[string]any{"summary": "s", "findings": []any{map[string]any{"detail": "d"}}}, "findings[0].title"},
		{"bad severity", map[string]any{"summary": "s", "findings": []any{map[string]any{"title": "t", "severity": "urgent"}}}, "findings[0].severity"},
		{"line zero", map[string]any{"summary": "s", "findings": []any{map[string]any{"title": "t", "line": 0.0}}}, "findings[0].line"},
		{"fractional line", map[string]any{"summary": "s", "findings": []any{map[string]any{"title": "t", "line": 2.5}}}, "findings[0].line"},
		{"unknown finding key", map[string]any{"summary": "s", "findings": []any{map[string]any{"title": "t", "fix": "x"}}}, "findings[0].fix"},
		{"unknown key", map[string]any{"summary": "s", "verdict": "ok"}, "verdict"},
		{"51 findings", map[string]any{"summary": "s", "findings": findings(51)}, "findings"},
		{"files not strings", map[string]any{"summary": "s", "files": []any{1.0}}, "files[0]"},
		{"201 files", map[string]any{"summary": "s", "files": make([]any, 201)}, "files"},
	}
	for _, c := range bad {
		res, err := tool.Execute(context.Background(), c.in, nil)
		if err != nil || !res.Error || !strings.Contains(res.Content, c.field) {
			t.Errorf("%s: result = %+v err = %v, want an error naming %q", c.name, res, err, c.field)
		}
		if h.get() != nil {
			t.Fatalf("%s: an invalid submission was stored", c.name)
		}
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"summary":  "Two issues.",
		"findings": []any{map[string]any{"title": "nil map", "severity": "high", "file": "a.go", "line": 12.0}},
		"files":    []any{"a.go"},
	}, nil)
	if err != nil || res.Error || !strings.Contains(res.Content, "Reply with TASK_COMPLETE") {
		t.Fatalf("valid submission: %+v %v", res, err)
	}
	got := h.get()
	if got == nil || got.Summary != "Two issues." || len(got.Findings) != 1 || got.Findings[0].Line != 12 || got.Findings[0].Severity != "high" || len(got.Files) != 1 {
		t.Fatalf("stored = %+v", got)
	}
}

func TestSubmitResultReplaces(t *testing.T) {
	h := &resultHolder{}
	tool := NewSubmitResultTool(h)
	for _, s := range []string{"first", "second"} {
		if res, _ := tool.Execute(context.Background(), map[string]any{"summary": s}, nil); res.Error {
			t.Fatalf("%s: %+v", s, res)
		}
	}
	got := h.get()
	if got == nil || got.Summary != "second" {
		t.Fatalf("stored = %+v, want the second submission", got)
	}
	// Arrays are never null in the JSON the parent sees.
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"findings":[]`) || !strings.Contains(string(b), `"files":[]`) {
		t.Fatalf("json = %s", b)
	}
}
