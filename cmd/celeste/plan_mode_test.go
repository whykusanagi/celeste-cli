package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// offeredTools is the tool names a recorded request offered.
func offeredTools(t *testing.T, req fakeprovider.Request) []string {
	t.Helper()
	var names []string
	list, _ := req.Body["tools"].([]any)
	for _, raw := range list {
		def, _ := raw.(map[string]any)
		fn, _ := def["function"].(map[string]any)
		if name, _ := fn["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// Review Focus 3.
func TestPlanModeGateDeniesWrites(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"no"}`}}},
		fakeprovider.Turn{Text: "ok"})
	_, deps, ws := chatApp(t, srv)
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("go"), Tools: true, Run: 1})
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("plan mode let a write through")
	}
	offered := offeredTools(t, srv.Requests()[0])
	if hasName(offered, "write_file") || hasName(offered, "bash") || hasName(offered, "todo") {
		t.Fatalf("plan mode offered a write tool: %v", offered)
	}
	if !hasName(offered, "read_file") || !hasName(offered, "submit_plan") {
		t.Fatalf("plan mode should offer read-only tools and submit_plan: %v", offered)
	}
	if !strings.Contains(fmt.Sprint(srv.Requests()[1].Body["messages"]), "plan mode is on") {
		t.Fatal("the denial should reach the model")
	}
}

// The refusal does not depend on the permission prompt: a prompt (or trust
// mode, or an always-allow rule) that would approve the write never sees
// the call.
func TestPlanModeRefusesEvenWhenThePromptWouldAllow(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"no"}`}}},
		fakeprovider.Turn{Text: "ok"})
	_, deps, ws := chatApp(t, srv)
	asked := false
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		asked = true
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("go"), Tools: true, Run: 1})
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("plan mode let a write through")
	}
	if asked {
		t.Fatal("a refused call reached the permission prompt")
	}
	if !strings.Contains(fmt.Sprint(srv.Requests()[1].Body["messages"]), "plan mode is on") {
		t.Fatal("the denial should reach the model")
	}
}

// Outside plan mode the full tool set is offered, without submit_plan.
func TestPlanModeOffKeepsTheFullToolSet(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	if deps.adapter.PlanMode() {
		t.Fatal("plan mode starts off")
	}
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("go"), Tools: true, Run: 1})
	offered := offeredTools(t, srv.Requests()[0])
	if !hasName(offered, "write_file") || hasName(offered, "submit_plan") {
		t.Fatalf("offered = %v", offered)
	}
}
