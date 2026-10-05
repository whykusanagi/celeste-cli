package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// #357: the todo list is the session's own bookkeeping. Creating and
// updating items runs without a permission prompt in the default mode.
func TestTodoUpdatesNeedNoApproval(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c", Name: "todo", Args: `{"action":"create","title":"step one"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "u", Name: "todo", Args: `{"action":"update","id":1,"status":"in_progress"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "d", Name: "todo", Args: `{"action":"update","id":1,"status":"done"}`}}},
		fakeprovider.Turn{Text: "done"})
	_, deps, ws := chatApp(t, srv)
	asked := 0
	deps.registry.SetPromptFunc(func(req tools.PermissionRequest) tools.PermissionResponse {
		asked++
		return tools.PermissionResponse{Decision: "deny"}
	})
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("track it"), Tools: true, Run: 1})
	if asked != 0 {
		t.Fatalf("the todo tool asked for permission %d times", asked)
	}
	items := builtin.NewTodoStore(ws).List()
	if len(items) != 1 || items[0].Status != "done" {
		t.Fatalf("todos = %+v", items)
	}
}

// #357: auto-allowed is not invisible: a PreToolUse hook still sees (and
// can block) a todo call.
func TestHooksStillSeeTodoCalls(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c", Name: "todo", Args: `{"action":"create","title":"step one"}`}}},
		fakeprovider.Turn{Text: "The hook stopped it."})
	m, _, _, ws := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreToolUse, "todo", "deny", "no todos here"))
	})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "track it"}},
		func(m tea.Model) bool { return strings.Contains(lastAssistant(m), "stopped") && turnIdle(m) }, 30*time.Second)
	if got := lastOfRole(requestMessages(t, srv, 1), "tool"); !strings.Contains(got, "Blocked by pre-tool hook: no todos here") {
		t.Fatalf("tool result sent to model = %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, ".celeste", "tasks.json")); err == nil {
		t.Fatal("a hook-denied todo call still wrote the list")
	}
}
