package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// trackedPlan is a planState tracking a two-step plan approved in this
// process, on store.
func trackedPlan(t *testing.T) (*planState, *builtin.TodoStore, []builtin.PlanStep) {
	t.Helper()
	store := builtin.NewTodoStore("")
	steps := []builtin.PlanStep{{Title: "write tests"}, {Title: "implement"}}
	for i := range steps {
		steps[i].TodoID = store.Create(steps[i].Title, "").ID
	}
	p := &planState{todos: store}
	p.track(steps)
	return p, store, steps
}

// The reminder comes after planIdleTurns tool turns (after the approval's
// own) without a change to the plan's todo items, names the open steps
// with their ids, and starts counting again after each reminder.
func TestPlanProgressReminderAfterIdleToolTurns(t *testing.T) {
	p, _, _ := trackedPlan(t)
	// The first call is the approval's own turn; then the idle ones.
	for i := 0; i < planIdleTurns; i++ {
		if _, ok := p.progressReminder(); ok {
			t.Fatalf("reminded after %d idle turns", i)
		}
	}
	r, ok := p.progressReminder()
	if !ok {
		t.Fatalf("no reminder after %d idle turns", planIdleTurns)
	}
	for _, want := range []string{"1. write tests (todo id 1, pending)", "2. implement (todo id 2, pending)", `"status":"done"`} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("reminder lacks %q:\n%s", want, r.Text)
		}
	}
	if r.Source != planReminderSource {
		t.Fatalf("source = %q", r.Source)
	}
	if _, ok := p.progressReminder(); ok {
		t.Fatal("a reminder restarts the count")
	}
}

// A status change resets the count; done steps drop out of the reminder;
// once every step is done the plan is no longer tracked.
func TestPlanProgressReminderFollowsTheTodoList(t *testing.T) {
	p, store, steps := trackedPlan(t)
	p.progressReminder()
	p.progressReminder()
	if _, err := store.Update(steps[0].TodoID, "done"); err != nil {
		t.Fatal(err)
	}
	// The turn that sees the change is not idle; the count starts after it.
	for i := 0; i < planIdleTurns; i++ {
		if _, ok := p.progressReminder(); ok {
			t.Fatalf("reminded %d turns after progress", i)
		}
	}
	r, ok := p.progressReminder()
	if !ok || strings.Contains(r.Text, "write tests") || !strings.Contains(r.Text, "2. implement (todo id 2, pending)") {
		t.Fatalf("reminder = %q, %v", r.Text, ok)
	}
	if _, err := store.Update(steps[1].TodoID, "done"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*planIdleTurns; i++ {
		if _, ok := p.progressReminder(); ok {
			t.Fatal("a finished plan needs no reminder")
		}
	}
}

// Removed todo items are not open: a plan whose items are all done or
// removed is no longer tracked.
func TestPlanProgressReminderDropsRemovedSteps(t *testing.T) {
	p, store, steps := trackedPlan(t)
	if err := store.Delete(steps[0].TodoID); err != nil {
		t.Fatal(err)
	}
	p.progressReminder() // the change
	for i := 1; i < planIdleTurns; i++ {
		p.progressReminder()
	}
	r, ok := p.progressReminder()
	if !ok || strings.Contains(r.Text, "write tests") {
		t.Fatalf("reminder = %q, %v", r.Text, ok)
	}
	if _, err := store.Update(steps[1].TodoID, "done"); err != nil {
		t.Fatal(err)
	}
	p.progressReminder()
	if p.tracking() {
		t.Fatal("done and removed: nothing left to track")
	}
}

// No reminder while planning (plan mode on) or without a plan approved in
// this process.
func TestPlanProgressReminderNeedsAnApprovedPlan(t *testing.T) {
	idle := &planState{todos: builtin.NewTodoStore("")}
	for i := 0; i < 2*planIdleTurns; i++ {
		if _, ok := idle.progressReminder(); ok {
			t.Fatal("no plan, no reminder")
		}
	}
	p, _, _ := trackedPlan(t)
	p.set(true, "")
	for i := 0; i < 2*planIdleTurns; i++ {
		if _, ok := p.progressReminder(); ok {
			t.Fatal("plan mode on: no reminder")
		}
	}
	var nilPlan *planState
	if _, ok := nilPlan.progressReminder(); ok {
		t.Fatal("nil plan state")
	}
}

// The wrapper is there only while it can matter, adds the plan reminder at
// BoundaryTools only, and works without inner steering.
func TestPlanSteeringAddsTheReminderAtToolBoundaries(t *testing.T) {
	p, _, _ := trackedPlan(t)
	s := withPlanProgress(nil, p)
	if s == nil {
		t.Fatal("a plan state needs the wrapper")
	}
	if withPlanProgress(nil, nil) != nil || withPlanProgress(nil, &planState{}) != nil {
		t.Fatal("no plan mode and no tracked plan: the inner steering (nil) unchanged")
	}
	planning := &planState{}
	planning.set(true, "")
	if withPlanProgress(nil, planning) == nil {
		t.Fatal("plan mode on: an approval this turn needs the wrapper")
	}
	s.Request(1, func() {})
	if s.Observe(loop.Event{Kind: loop.EventTextDelta, Text: "x"}) || s.Calls(1, nil) {
		t.Fatal("the wrapper never interrupts on its own")
	}
	if se, ok := s.(loop.StreamEnder); ok && se.EndStream() {
		t.Fatal("the wrapper never interrupts on its own")
	}
	for i := 0; i < 2*planIdleTurns; i++ {
		if rs := s.Reminders(loop.BoundaryRetry); len(rs) != 0 {
			t.Fatal("retries get no plan reminder")
		}
		if rs := s.Reminders(loop.BoundaryRun); len(rs) != 0 {
			t.Fatal("a run's start gets no plan reminder")
		}
	}
	got := 0
	for i := 0; i <= planIdleTurns; i++ { // the approval's turn, then the idle ones
		got += len(s.Reminders(loop.BoundaryTools))
	}
	if got != 1 {
		t.Fatalf("reminders at tool boundaries = %d, want 1", got)
	}
}

// #325 end to end: after approval, a model that works through tool turns
// without ticking a step gets a hidden reminder naming the open steps and
// their todo ids; one that ticks as it goes gets none.
func TestApprovedPlanRemindsAModelThatForgetsTheTodos(t *testing.T) {
	read := func(id, path string) fakeprovider.Turn {
		return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "read_file", Args: fmt.Sprintf(`{"path":%q}`, path)}}}
	}
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "submit_plan",
			Args: `{"steps":[{"title":"write tests"},{"title":"implement"}]}`}}},
		read("r1", "a.txt"), read("r2", "b.txt"), read("r3", "c.txt"), read("r4", "d.txt"),
		fakeprovider.Turn{Text: "done"})
	_, deps, ws := chatApp(t, srv)
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(ws, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deps.registry.SetAskFunc(func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("plan it"), Tools: true, Run: 1})
	reqs := srv.Requests()
	if len(reqs) != 6 {
		t.Fatalf("requests = %d", len(reqs))
	}
	first := -1
	for i, r := range reqs {
		if strings.Contains(fmt.Sprint(r.Body["messages"]), "2. implement (todo id 2, pending)") {
			first = i
			break
		}
	}
	// Request 1 follows the approval; requests 2..4 follow idle tool turns.
	if want := 1 + planIdleTurns; first != want {
		t.Fatalf("the reminder first reached request %d, want %d", first, want)
	}
}

func TestApprovedPlanTickedAsItGoesGetsNoReminder(t *testing.T) {
	tick := func(id string, todo int, status string) fakeprovider.Turn {
		return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "todo",
			Args: fmt.Sprintf(`{"action":"update","id":%d,"status":%q}`, todo, status)}}}
	}
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "submit_plan",
			Args: `{"steps":[{"title":"write tests"},{"title":"implement"}]}`}}},
		tick("t1", 1, "in_progress"), tick("t2", 1, "done"), tick("t3", 2, "in_progress"), tick("t4", 2, "done"),
		fakeprovider.Turn{Text: "all done"})
	_, deps, _ := chatApp(t, srv)
	deps.registry.SetAskFunc(func(context.Context, tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Selected: []string{"Approve and start"}}, nil
	})
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	deps.adapter.SetPlanMode(true, "")
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("plan it"), Tools: true, Run: 1})
	for i, r := range srv.Requests() {
		if strings.Contains(fmt.Sprint(r.Body["messages"]), "system-reminder") {
			t.Fatalf("request %d carried a reminder", i)
		}
	}
}
