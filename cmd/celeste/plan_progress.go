package main

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
)

// planIdleTurns is how many tool turns may pass without a change to an
// approved plan's todo items before the model is reminded to tick them
// (#325).
const planIdleTurns = 3

// planReminderSource is the plan reminder's loop.Reminder source.
const planReminderSource = "plan"

// track starts following the steps of a plan approved in this process: the
// progress reminder watches their todo items until every one is done.
func (p *planState) track(steps []builtin.PlanStep) {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	p.steps = append([]builtin.PlanStep(nil), steps...)
	// The approval's own tool turn is the first one counted: it is not idle.
	p.idle = -1
	p.last, _ = p.planStatus()
}

// planStatus is the tracked steps' todo statuses ("-": removed) as one
// comparable string, and whether a step is still open. Callers hold pmu.
func (p *planState) planStatus() (string, bool) {
	status := map[int]string{}
	if p.todos != nil {
		for _, it := range p.todos.List() {
			status[it.ID] = it.Status
		}
	}
	var b strings.Builder
	open := false
	for _, s := range p.steps {
		st, ok := status[s.TodoID]
		if !ok {
			st = "-"
		} else if st != "done" {
			open = true
		}
		fmt.Fprintf(&b, "%d=%s;", s.TodoID, st)
	}
	return b.String(), open
}

// progressReminder is called after each tool turn. While a plan approved
// in this process has open steps, it counts the tool turns that leave the
// plan's todo items unchanged and, at planIdleTurns, returns a reminder
// naming the open steps with their todo ids (the count then restarts).
// Nothing while plan mode is on, without a todo tool, or once every step
// is done (the plan is then no longer tracked).
func (p *planState) progressReminder() (loop.Reminder, bool) {
	if p == nil || p.active() {
		return loop.Reminder{}, false
	}
	p.pmu.Lock()
	defer p.pmu.Unlock()
	if len(p.steps) == 0 || p.todos == nil {
		return loop.Reminder{}, false
	}
	snap, open := p.planStatus()
	if !open {
		p.steps = nil
		return loop.Reminder{}, false
	}
	if snap != p.last {
		p.last, p.idle = snap, 0
		return loop.Reminder{}, false
	}
	p.idle++
	if p.idle < planIdleTurns {
		return loop.Reminder{}, false
	}
	p.idle = 0
	return loop.Reminder{Source: planReminderSource, Text: p.reminderText()}, true
}

// reminderText lists the plan's open steps. Callers hold pmu.
func (p *planState) reminderText() string {
	status := map[int]string{}
	for _, it := range p.todos.List() {
		status[it.ID] = it.Status
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are carrying out an approved plan, and its todo list has not changed in %d tool turns. ", planIdleTurns)
	b.WriteString("If you have finished a step, mark it now with the todo tool: " +
		`{"action":"update","id":<id>,"status":"done"}` + "; mark the step you are on " +
		`{"action":"update","id":<id>,"status":"in_progress"}` + ". Open steps:")
	for i, s := range p.steps {
		st, ok := status[s.TodoID]
		if !ok || st == "done" {
			continue
		}
		fmt.Fprintf(&b, "\n%d. %s (todo id %d, %s)", i+1, s.Title, s.TodoID, st)
	}
	return b.String()
}

// planSteering adds the plan progress reminder to a turn's steering (stream
// rules and the watchdog, when the chat has them) at tool boundaries.
type planSteering struct {
	inner loop.Steering // nil: no stream rules or watchdog
	plan  *planState
}

// withPlanProgress wraps inner with the plan reminder when this turn can
// need it: plan mode is on (an approval mid-turn starts tracking) or an
// approved plan is tracked. Otherwise it is inner unchanged, so a chat
// without a plan steers exactly as before.
func withPlanProgress(inner loop.Steering, plan *planState) loop.Steering {
	if !plan.active() && !plan.tracking() {
		return inner
	}
	return &planSteering{inner: inner, plan: plan}
}

// tracking reports whether a plan approved in this process still has its
// steps followed by the progress reminder.
func (p *planState) tracking() bool {
	if p == nil {
		return false
	}
	p.pmu.Lock()
	defer p.pmu.Unlock()
	return len(p.steps) > 0
}

func (s *planSteering) Request(turn int, interrupt func()) {
	if s.inner != nil {
		s.inner.Request(turn, interrupt)
	}
}

func (s *planSteering) Observe(ev loop.Event) bool {
	return s.inner != nil && s.inner.Observe(ev)
}

func (s *planSteering) Calls(turn int, calls []loop.ToolCall) bool {
	return s.inner != nil && s.inner.Calls(turn, calls)
}

// EndStream forwards to inner when it batches its scans (loop.StreamEnder).
func (s *planSteering) EndStream() bool {
	se, ok := s.inner.(loop.StreamEnder)
	return ok && se.EndStream()
}

func (s *planSteering) Reminders(b loop.Boundary) []loop.Reminder {
	var out []loop.Reminder
	if s.inner != nil {
		out = s.inner.Reminders(b)
	}
	if b == loop.BoundaryTools {
		if r, ok := s.plan.progressReminder(); ok {
			out = append(out, r)
		}
	}
	return out
}

var _ loop.StreamEnder = (*planSteering)(nil)
