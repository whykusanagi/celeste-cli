package main

import (
	"sync"
	"sync/atomic"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// planModeRefusal is what a call outside the plan-mode tool set gets (2.0
// W4e ruling 8).
const planModeRefusal = "plan mode is on: only read-only tools until the plan is approved (/plan off to leave)"

// planOnlyRefusal is what submit_plan gets outside plan mode, where it is
// not offered.
const planOnlyRefusal = "submit_plan is only for plan mode (the user starts it with /plan)"

// planState is the chat's plan mode (ruling 7): on is read by each
// request's tool list and each call's refusal check, so an approval
// mid-turn opens the full tool set for the rest of that turn.
type planState struct {
	on   atomic.Bool
	mu   sync.Mutex
	goal string // /plan <goal>, the default goal for submit_plan

	// todos is the todo tool's store (nil without one); the progress
	// reminder reads the approved plan's items from it.
	todos *builtin.TodoStore
	// pmu guards the progress reminder's state: the steps of the plan
	// approved in this process, their last todo statuses, and the tool
	// turns since those changed.
	pmu   sync.Mutex
	steps []builtin.PlanStep
	last  string
	idle  int
}

func (p *planState) set(on bool, goal string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !on {
		goal = ""
	}
	p.goal = goal
	p.on.Store(on)
}

func (p *planState) active() bool { return p != nil && p.on.Load() }

func (p *planState) currentGoal() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.goal
}

// registerSubmitPlan puts submit_plan on the chat's registry (chat only:
// agent, MCP and subagent registries never have it). The registry's ask
// function is read at call time, so the modal runTUI installs later
// answers; approving leaves plan mode and starts the progress reminder.
func registerSubmitPlan(reg *tools.Registry, workspace string, plan *planState) {
	t := builtin.NewSubmitPlanTool(workspace, reg.Ask, func(p builtin.PlanFile) {
		plan.set(false, "")
		plan.track(p.Steps)
	})
	t.DefaultGoal = plan.currentGoal
	// One todo list: the todo tool keeps it in memory.
	if tt, ok := reg.Get("todo"); ok {
		if todo, ok := tt.(*builtin.TodoTool); ok {
			t.Todos = todo.Store()
			plan.todos = t.Todos
		}
	}
	reg.RegisterWithModes(t, tools.ModeChat)
}

// planToolAllowed reports whether plan mode lets the model see and call
// name: read-only tools and submit_plan.
func planToolAllowed(reg *tools.Registry, name string) bool {
	if name == builtin.SubmitPlanName {
		return true
	}
	t, ok := reg.Get(name)
	return ok && t.IsReadOnly()
}

// planRefusal is the chat loop's Refuse: while plan mode is on, a call to a
// tool that is not read-only (and not submit_plan) is refused before it
// reaches the registry, whatever the permission mode or rules would say;
// outside plan mode only submit_plan is. Allowed calls still go through
// the registry's hooks and permission checks unchanged.
func planRefusal(plan *planState, reg *tools.Registry) func(string) string {
	return func(name string) string {
		if !plan.active() {
			if name == builtin.SubmitPlanName {
				return planOnlyRefusal
			}
			return ""
		}
		if planToolAllowed(reg, name) {
			return ""
		}
		return planModeRefusal
	}
}

// planFilter is the plan-mode view of a tool list: in plan mode, only
// read-only tools and submit_plan; otherwise everything but submit_plan.
func planFilter(defs []tui.SkillDefinition, plan *planState, reg *tools.Registry) []tui.SkillDefinition {
	on := plan.active()
	out := make([]tui.SkillDefinition, 0, len(defs))
	for _, d := range defs {
		if on && !planToolAllowed(reg, d.Name) {
			continue
		}
		if !on && d.Name == builtin.SubmitPlanName {
			continue
		}
		out = append(out, d)
	}
	return out
}

// SetPlanMode turns plan mode on (with the /plan goal, if any) or off
// (tui.PlanModer).
func (a *TUIClientAdapter) SetPlanMode(on bool, goal string) {
	if a.plan == nil {
		return
	}
	a.plan.set(on, goal)
}

// PlanMode reports whether plan mode is on (tui.PlanModer).
func (a *TUIClientAdapter) PlanMode() bool { return a.plan.active() }

var _ tui.PlanModer = (*TUIClientAdapter)(nil)
