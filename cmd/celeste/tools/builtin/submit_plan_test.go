package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func TestSubmitPlanApproveWritesPlanAndTodos(t *testing.T) {
	ws := t.TempDir()
	approved := false
	var asked tools.AskRequest
	ask := func(_ context.Context, req tools.AskRequest) (tools.AskResponse, error) {
		asked = req
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) { approved = true })
	res, _ := tool.Execute(context.Background(), map[string]any{"goal": "ship", "steps": []any{
		map[string]any{"title": "write tests"}, map[string]any{"title": "implement", "detail": "minimal"},
	}}, nil)
	if res.Error || !approved || !strings.Contains(res.Content, "Plan mode is off") {
		t.Fatalf("result = %+v approved=%v", res, approved)
	}
	if !strings.HasPrefix(asked.Question, "Approve this plan?\n\n") || !strings.Contains(asked.Question, "1. write tests") ||
		!strings.Contains(asked.Question, "2. implement") {
		t.Fatalf("question = %q", asked.Question)
	}
	// The safe answer comes first: the ask modal pre-selects the first
	// option, and Enter on the default must never approve.
	if len(asked.Options) != 2 || asked.Options[0].Label != "Keep planning" || asked.Options[1].Label != "Approve and start" {
		t.Fatalf("options = %+v", asked.Options)
	}
	plan, err := LoadPlan(ws)
	if err != nil || len(plan.Steps) != 2 || plan.Steps[1].TodoID == 0 {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	if plan.Goal != "ship" || plan.Steps[1].Detail != "minimal" || plan.ApprovedAt.IsZero() {
		t.Fatalf("plan = %+v", plan)
	}
	if items := NewTodoStore(ws).List(); len(items) != 2 || items[0].Title != "write tests" {
		t.Fatalf("todos = %+v", items)
	}
	if !strings.Contains(res.Content, "2 todo items created") {
		t.Fatalf("result = %q", res.Content)
	}
	info, err := os.Stat(filepath.Join(ws, ".celeste", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("plan.json mode = %v", info.Mode())
	}
}

func TestSubmitPlanKeepPlanning(t *testing.T) {
	ws := t.TempDir()
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Keep planning"}}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) { t.Fatal("must not approve") })
	res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil)
	if res.Error || !strings.Contains(res.Content, "keep planning") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := LoadPlan(ws); err == nil {
		t.Fatal("no plan file before approval")
	}
	if items := NewTodoStore(ws).List(); len(items) != 0 {
		t.Fatalf("todos = %+v", items)
	}
}

// A cancelled modal (Esc, the turn ended) keeps planning too.
func TestSubmitPlanCancelledKeepsPlanning(t *testing.T) {
	ws := t.TempDir()
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Cancelled: true}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) { t.Fatal("must not approve") })
	res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil)
	if res.Error || !strings.Contains(res.Content, "keep planning") {
		t.Fatalf("result = %+v", res)
	}
}

// Headless (no ask function installed): a clean error, nothing written.
func TestSubmitPlanHeadless(t *testing.T) {
	ws := t.TempDir()
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{}, errors.New("interactive input unavailable in this context")
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) { t.Fatal("must not approve") })
	res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil)
	if !res.Error || !strings.Contains(res.Content, "plan mode needs the interactive chat") {
		t.Fatalf("result = %+v", res)
	}
	tool = NewSubmitPlanTool(ws, nil, func(PlanFile) { t.Fatal("must not approve") })
	res, _ = tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil)
	if !res.Error || !strings.Contains(res.Content, "plan mode needs the interactive chat") {
		t.Fatalf("nil ask: result = %+v", res)
	}
}

func TestSubmitPlanValidatesSteps(t *testing.T) {
	ws := t.TempDir()
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		t.Fatal("an invalid plan must not reach the user")
		return tools.AskResponse{}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) {})
	many := make([]any, 31)
	for i := range many {
		many[i] = map[string]any{"title": "s"}
	}
	for name, input := range map[string]map[string]any{
		"none":        {},
		"empty":       {"steps": []any{}},
		"too many":    {"steps": many},
		"blank title": {"steps": []any{map[string]any{"title": "  "}}},
		"not object":  {"steps": []any{"just a string"}},
		"bad detail":  {"steps": []any{map[string]any{"title": "x", "detail": 3}}},
		"bad goal":    {"goal": 5, "steps": []any{map[string]any{"title": "x"}}},
	} {
		res, err := tool.Execute(context.Background(), input, nil)
		if err != nil || !res.Error {
			t.Errorf("%s: result = %+v, err = %v; want an error result", name, res, err)
		}
	}
}

func TestSubmitPlanUsesDefaultGoal(t *testing.T) {
	ws := t.TempDir()
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) {})
	tool.DefaultGoal = func() string { return "add caching" }
	if res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil); res.Error {
		t.Fatalf("result = %+v", res)
	}
	plan, err := LoadPlan(ws)
	if err != nil || plan.Goal != "add caching" {
		t.Fatalf("plan = %+v %v", plan, err)
	}
}

// A plan that cannot be saved leaves no todo items behind.
func TestSubmitPlanRollsBackTodosWhenThePlanCannotBeSaved(t *testing.T) {
	ws := t.TempDir()
	store := NewTodoStore(ws)
	store.Create("existing", "")
	// A directory where plan.json goes makes the write fail.
	if err := os.MkdirAll(PlanPath(ws), 0o755); err != nil {
		t.Fatal(err)
	}
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) { t.Fatal("must not approve") })
	tool.Todos = store
	res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{map[string]any{"title": "x"}}}, nil)
	if !res.Error || !strings.Contains(res.Content, "could not save the plan") {
		t.Fatalf("result = %+v", res)
	}
	if items := NewTodoStore(ws).List(); len(items) != 1 || items[0].Title != "existing" {
		t.Fatalf("todos = %+v", items)
	}
}

// #325: the approval tells the model which todo id each step became and
// how to tick it, so it updates the list as it works. The ids are the
// store's real ones, not 1..n.
func TestSubmitPlanApprovalTellsTheModelHowToTickSteps(t *testing.T) {
	ws := t.TempDir()
	store := NewTodoStore(ws)
	store.Create("existing", "")
	ask := func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	}
	tool := NewSubmitPlanTool(ws, ask, func(PlanFile) {})
	tool.Todos = store
	res, _ := tool.Execute(context.Background(), map[string]any{"steps": []any{
		map[string]any{"title": "write tests"}, map[string]any{"title": "implement"},
	}}, nil)
	if res.Error || !strings.HasPrefix(res.Content, "Plan approved: 2 todo items created") {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{
		"1. write tests (todo id 2)",
		"2. implement (todo id 3)",
		`{"action":"update","id":<id>,"status":"in_progress"}`,
		`{"action":"update","id":<id>,"status":"done"}`,
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("approval lacks %q:\n%s", want, res.Content)
		}
	}
}

// #325: the todo tool's description says plan steps are todo items to tick.
func TestTodoDescriptionCoversPlanSteps(t *testing.T) {
	d := NewTodoTool("").Description()
	for _, want := range []string{"submit_plan", "in_progress when you start", "done as soon as you finish"} {
		if !strings.Contains(d, want) {
			t.Errorf("todo description lacks %q:\n%s", want, d)
		}
	}
}
