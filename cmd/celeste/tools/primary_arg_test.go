package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

// The registry checks an argument-scoped rule against the field the tool
// declares and acts on, so an undeclared decoy field decides nothing.
func TestRegistryRulesUseTheDeclaredPrimaryArgument(t *testing.T) {
	ran := false
	tool := &mockTool{
		name:   "write_file",
		params: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`),
		executeFunc: func(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
			ran = true
			return ToolResult{Content: "ok"}, nil
		},
	}
	r := NewRegistry()
	r.Register(tool)
	r.SetPermissionChecker(permissions.NewChecker(permissions.PermissionConfig{
		Mode:       permissions.ModeTrust,
		AlwaysDeny: []permissions.Rule{{ToolPattern: "write_file(secrets/*)", Decision: permissions.Deny}},
	}))
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "secrets/key", "content": "x", "command": "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if ran || !res.Error {
		t.Fatalf("a decoy field dodged the deny rule: ran=%v %q", ran, res.Content)
	}

	for want, params := range map[string]string{
		"command": `{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"number"}}}`,
		"path":    `{"type":"object","properties":{"content":{"type":"string"},"path":{"type":"string"}}}`,
		"url":     `{"type":"object","properties":{"url":{"type":"string"},"format":{"type":"string"}}}`,
		"text":    `{"type":"object","properties":{"text":{"type":"string"},"max":{"type":"number"}}}`,
		"":        `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}}}`,
	} {
		if got := PrimaryArg(&mockTool{params: json.RawMessage(params)}); got != want {
			t.Errorf("PrimaryArg(%s) = %q, want %q", params, got, want)
		}
	}
}
