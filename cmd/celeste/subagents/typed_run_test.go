package subagents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// splitTypedResult splits spawn_agent's typed result into its status line
// and the result JSON.
func splitTypedResult(t *testing.T, content string) (string, Result) {
	t.Helper()
	head, body, ok := strings.Cut(content, "\n")
	if !ok {
		t.Fatalf("no status line in %q", content)
	}
	var r Result
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &r); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, content)
	}
	return head, r
}

func TestTypedRunReturnsTheSubmittedResult(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "s1", Name: "submit_result",
			Args: `{"summary":"Two issues.","findings":[{"title":"nil map","severity":"high","file":"a.go","line":3}],"files":["a.go"]}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE"})
	m, _, _, _ := fakeManager(t, srv)
	res, err := NewSpawnAgentTool(m).Execute(context.Background(), map[string]any{"goal": "look for bugs", "type": "explore"}, nil)
	if err != nil || res.Error {
		t.Fatalf("spawn: %+v %v", res, err)
	}
	head, r := splitTypedResult(t, res.Content)
	if !strings.HasPrefix(head, "subagent ") || !strings.Contains(head, "(explore): completed") {
		t.Fatalf("status line = %q", head)
	}
	if r.Summary != "Two issues." || len(r.Findings) != 1 || r.Findings[0].Line != 3 || len(r.Files) != 1 || r.Warning != "" {
		t.Fatalf("result = %+v", r)
	}
	runs := m.ListRuns()
	if len(runs) != 1 || runs[0].Type != TypeExplore || !strings.Contains(runs[0].Result, `"summary": "Two issues."`) || runs[0].Summary != "Two issues." {
		t.Fatalf("run = %+v", runs)
	}

	// The explore run was offered submit_result and no write tools, and
	// its goal says how to finish.
	body := srv.Requests()[0].Body
	offered := jsonString(body["tools"])
	if !strings.Contains(offered, `"submit_result"`) || strings.Contains(offered, `"write_file"`) || strings.Contains(offered, `"bash"`) {
		t.Fatalf("explore offered %s", offered)
	}
	if msgs := jsonString(body["messages"]); !strings.Contains(msgs, "submit_result") || strings.Contains(msgs, "Voice Boundary") {
		t.Fatal("the explore goal should name submit_result and carry no persona")
	}
	// The second request carries the tool's reply.
	if !strings.Contains(jsonString(srv.Requests()[1].Body["messages"]), "Result recorded") {
		t.Fatal("submit_result's reply did not reach the model")
	}
}

// Review Focus 2: a run that never calls submit_result still hands the
// parent the documented JSON, with a warning.
func TestUntypedEndingWrapsTheFinalText(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Found two issues.\nTASK_COMPLETE"})
	m, _, _, _ := fakeManager(t, srv)
	res, err := NewSpawnAgentTool(m).Execute(context.Background(), map[string]any{"goal": "review the diff", "type": "review"}, nil)
	if err != nil || res.Error {
		t.Fatalf("spawn: %+v %v", res, err)
	}
	head, r := splitTypedResult(t, res.Content)
	if !strings.Contains(head, "(review): completed") {
		t.Fatalf("status line = %q", head)
	}
	if r.Summary != "Found two issues." || r.Findings == nil || len(r.Findings) != 0 || r.Files == nil || len(r.Files) != 0 {
		t.Fatalf("result = %+v", r)
	}
	if r.Warning != "the subagent did not call submit_result" {
		t.Fatalf("warning = %q", r.Warning)
	}
	if !strings.Contains(res.Content, `"findings": []`) || !strings.Contains(res.Content, `"files": []`) {
		t.Fatalf("arrays must be empty, not null:\n%s", res.Content)
	}
}

// A general spawn (no type argument) is typed too: it gets submit_result
// and every other tool.
func TestGeneralSpawnIsTyped(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"})
	m, _, _, _ := fakeManager(t, srv)
	res, _ := NewSpawnAgentTool(m).Execute(context.Background(), map[string]any{"goal": "write it"}, nil)
	head, r := splitTypedResult(t, res.Content)
	if !strings.Contains(head, "(general): completed") || r.Summary != "wrote it" {
		t.Fatalf("head = %q result = %+v", head, r)
	}
	offered := jsonString(srv.Requests()[0].Body["tools"])
	if !strings.Contains(offered, `"submit_result"`) || !strings.Contains(offered, `"write_file"`) {
		t.Fatalf("general offered %s", offered)
	}
}

// Manager.Spawn without a type keeps the plain-text result.
func TestUntypedSpawnKeepsText(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "plain done"})
	m, _, ws, _ := fakeManager(t, srv)
	run, err := m.Spawn(context.Background(), "x", ws)
	if err != nil || run.Result != "plain done" || run.Type != "" {
		t.Fatalf("run = %+v %v", run, err)
	}
	if strings.Contains(jsonString(srv.Requests()[0].Body["tools"]), `"submit_result"`) {
		t.Fatal("an untyped run has no submit_result")
	}
}

func TestStripCompletionMarker(t *testing.T) {
	for in, want := range map[string]string{
		"Found two issues.\nTASK_COMPLETE":    "Found two issues.",
		"TASK_COMPLETE: all good":             "all good",
		"**TASK_COMPLETE**\nnotes":            "notes",
		"TASK_COMPLETED is a different token": "TASK_COMPLETED is a different token",
		"TASK_COMPLETE":                       "",
	} {
		if got := stripCompletionMarker(in); got != want {
			t.Errorf("stripCompletionMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

// A typed run that submitted a result and then ran out of turns hands the
// parent that result, with why it stopped as the warning.
func TestFailedRunKeepsTheSubmittedResult(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "s1", Name: "submit_result", Args: `{"summary":"Half done."}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "list_files", Args: `{}`}}})
	m, _, _, _ := fakeManager(t, srv)
	res, _ := NewSpawnAgentTool(m).Execute(context.Background(), map[string]any{"goal": "look", "type": "explore", "max_turns": 1.0}, nil)
	if !res.Error {
		t.Fatalf("a run out of turns should fail: %+v", res)
	}
	head, r := splitTypedResult(t, res.Content)
	if !strings.Contains(head, "(explore): failed") || r.Summary != "Half done." || !strings.Contains(r.Warning, "stopped before finishing") {
		t.Fatalf("head = %q result = %+v", head, r)
	}
}

// A typed run that fails without submitting still hands the parent the
// typed JSON: its last reply as the summary, and why it stopped.
func TestFailedRunWithoutSubmitIsTyped(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "Looked at a.go so far.", ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "list_files", Args: `{}`}}})
	m, _, _, _ := fakeManager(t, srv)
	res, _ := NewSpawnAgentTool(m).Execute(context.Background(), map[string]any{"goal": "look", "type": "review", "max_turns": 1.0}, nil)
	if !res.Error {
		t.Fatalf("a run out of turns should fail: %+v", res)
	}
	head, r := splitTypedResult(t, res.Content)
	if !strings.Contains(head, "(review): failed") || r.Summary == "" ||
		!strings.Contains(r.Warning, "stopped before finishing") || !strings.Contains(r.Warning, "did not call submit_result") {
		t.Fatalf("head = %q result = %+v", head, r)
	}
}
