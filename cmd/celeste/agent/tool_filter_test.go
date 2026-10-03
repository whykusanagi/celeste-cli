package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// A typed subagent's runner (2.0 W4e): ToolFilter trims the run's own
// registry (never the parent's) to what the type allows, ExtraTools join
// before the filter, and the off persona level composes Celeste's identity,
// honesty rule and voice boundary in place of the full persona while the
// agent contract stays.
func TestToolFilterAndPersonaOff(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"}, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	parent := mustParentEnv(t, fakeCfg(srv), ws, func(string) {})

	opts := nestedOpts(ws, parent, func(string) {})
	opts.ExtraTools = []tools.Tool{extraTool{}}
	opts.ToolFilter = func(tl tools.Tool) bool { return tl.Name() == "read_file" || tl.Name() == "extra_probe" }
	opts.PersonaLevel = prompts.PersonaOff
	r, err := NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunGoal(context.Background(), "go"); err != nil {
		r.Close()
		t.Fatal(err)
	}
	r.Close()
	if _, ok := parent.Registry.Get("write_file"); !ok {
		t.Fatal("the filter must not touch the parent's registry")
	}
	body := srv.Requests()[0].Body
	offered := toJSONString(body["tools"])
	if !strings.Contains(offered, `"read_file"`) || !strings.Contains(offered, `"extra_probe"`) || strings.Contains(offered, `"write_file"`) {
		t.Fatalf("offered tools = %s", offered)
	}
	msgs := toJSONString(body["messages"])
	if !strings.Contains(msgs, "Voice Boundary") || !strings.Contains(msgs, "You are Celeste, the AI companion") {
		t.Fatalf("the off level must keep the identity and voice boundary: %s", msgs)
	}
	if !strings.Contains(msgs, "Execution Contract") {
		t.Fatal("the agent contract must stay")
	}

	// By default the full persona is composed (the general type).
	opts = nestedOpts(ws, parent, func(string) {})
	r, err = NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunGoal(context.Background(), "go"); err != nil {
		r.Close()
		t.Fatal(err)
	}
	r.Close()
	fullMsgs := toJSONString(srv.Requests()[1].Body["messages"])
	if !strings.Contains(fullMsgs, "Voice Boundary") || len(fullMsgs) <= len(msgs) {
		t.Fatal("the full persona should be composed by default, and be larger than off")
	}
}

type extraTool struct{}

func (extraTool) Name() string                               { return "extra_probe" }
func (extraTool) Description() string                        { return "probe" }
func (extraTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (extraTool) IsConcurrencySafe(map[string]any) bool      { return true }
func (extraTool) IsReadOnly() bool                           { return true }
func (extraTool) ValidateInput(map[string]any) error         { return nil }
func (extraTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (extraTool) Execute(context.Context, map[string]any, chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}
