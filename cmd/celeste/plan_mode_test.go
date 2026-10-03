package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
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

// Review Focus 4: approving the plan mid-turn offers the full tool set on
// the same turn's next request; the todo list holds the steps.
func TestApprovingThePlanEndsPlanModeMidTurn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "submit_plan",
			Args: `{"goal":"ship","steps":[{"title":"write tests"},{"title":"implement"}]}`}}},
		fakeprovider.Turn{Text: "starting step 1"})
	_, deps, ws := chatApp(t, srv)
	var asked tools.AskRequest
	deps.registry.SetAskFunc(func(_ context.Context, req tools.AskRequest) (tools.AskResponse, error) {
		asked = req
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("plan it"), Tools: true, Run: 1})
	if !strings.HasPrefix(asked.Question, "Approve this plan?") {
		t.Fatalf("the user was not asked: %+v", asked)
	}
	if deps.adapter.PlanMode() {
		t.Fatal("approval should end plan mode")
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if hasName(offeredTools(t, reqs[0]), "write_file") {
		t.Fatal("the first request was in plan mode")
	}
	if !hasName(offeredTools(t, reqs[1]), "write_file") {
		t.Fatal("after approval the same turn should offer write_file")
	}
	if !strings.Contains(fmt.Sprint(reqs[1].Body["messages"]), "Plan approved: 2 todo items created") {
		t.Fatal("the model should see the approval")
	}
	if items := builtin.NewTodoStore(ws).List(); len(items) != 2 || items[1].Title != "implement" {
		t.Fatalf("todos = %+v", items)
	}
	if _, err := builtin.LoadPlan(ws); err != nil {
		t.Fatal(err)
	}
}

// "Keep planning" keeps plan mode for the rest of the turn.
func TestKeepPlanningStaysInPlanMode(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "submit_plan", Args: `{"steps":[{"title":"x"}]}`}}},
		fakeprovider.Turn{Text: "revising"})
	_, deps, ws := chatApp(t, srv)
	deps.registry.SetAskFunc(func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Keep planning"}}, nil
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("plan it"), Tools: true, Run: 1})
	if !deps.adapter.PlanMode() {
		t.Fatal("keep planning should stay in plan mode")
	}
	if hasName(offeredTools(t, srv.Requests()[1]), "write_file") {
		t.Fatal("still planning: no write tools")
	}
	if _, err := os.Stat(builtin.PlanPath(ws)); err == nil {
		t.Fatal("no plan file before approval")
	}
}

// Review Focus 5: submit_plan exists only on the chat's registry.
func TestSubmitPlanOnlyInTheChat(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	_, deps, ws := chatApp(t, srv)
	if _, ok := deps.registry.Get("submit_plan"); !ok {
		t.Fatal("the chat should have submit_plan")
	}
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	for _, mode := range []loop.Mode{loop.ModeAgent, loop.ModeMCPChat} {
		env, err := loop.Setup(mode, cfg, ws, loop.SetupOptions{Warn: func(string) {}})
		if err != nil {
			t.Fatal(err)
		}
		_, ok := env.Registry.Get("submit_plan")
		env.Close()
		if ok {
			t.Fatalf("%v mode registered submit_plan", mode)
		}
	}
}

// writePlanFixture writes an approved two-step plan with todo items, the
// first done.
func writePlanFixture(t *testing.T, ws string) {
	t.Helper()
	store := builtin.NewTodoStore(ws)
	a := store.Create("write tests", "")
	b := store.Create("implement", "minimal")
	if _, err := store.Update(a.ID, "done"); err != nil {
		t.Fatal(err)
	}
	plan := builtin.PlanFile{Goal: "ship", Steps: []builtin.PlanStep{
		{Title: "write tests", TodoID: a.ID}, {Title: "implement", Detail: "minimal", TodoID: b.ID}}}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(builtin.PlanPath(ws), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// /plan show (the adapter's ShowPlan) renders the plan with todo status.
func TestShowPlanRendersTodoStatus(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	_, deps, ws := chatApp(t, srv)
	if got := deps.adapter.ShowPlan(); got != "No plan yet." {
		t.Fatalf("empty workspace: %q", got)
	}
	writePlanFixture(t, ws)
	got := deps.adapter.ShowPlan()
	for _, want := range []string{"Plan: ship", "[x] 1. write tests", "[ ] 2. implement", "minimal"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ShowPlan lacks %q:\n%s", want, got)
		}
	}
}

// The plan's todo items and the todo tool share one store: a todo call
// after approval (the same turn) keeps the plan's items.
func TestApprovedTodosSurviveALaterTodoCall(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "submit_plan", Args: `{"steps":[{"title":"a"},{"title":"b"}]}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "t", Name: "todo", Args: `{"action":"update","id":1,"status":"in_progress"}`}}},
		fakeprovider.Turn{Text: "working"})
	_, deps, ws := chatApp(t, srv)
	deps.registry.SetAskFunc(func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	})
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("plan it"), Tools: true, Run: 1})
	items := builtin.NewTodoStore(ws).List()
	if len(items) != 2 || items[0].Status != "in_progress" {
		t.Fatalf("todos = %+v", items)
	}
}

// The adapter's own tool list (the skills panel's count) follows plan mode
// like the loop's requests do: no submit_plan while it is off.
func TestAdapterSkillsFollowPlanMode(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _ := chatApp(t, srv)
	names := func() []string {
		var out []string
		for _, d := range deps.adapter.GetSkills() {
			out = append(out, d.Name)
		}
		return out
	}
	if off := names(); !hasName(off, "write_file") || hasName(off, "submit_plan") {
		t.Fatalf("plan mode off: skills = %v", off)
	}
	deps.adapter.SetPlanMode(true, "")
	if on := names(); hasName(on, "write_file") || !hasName(on, "submit_plan") {
		t.Fatalf("plan mode on: skills = %v", on)
	}
}
