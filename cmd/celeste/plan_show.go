package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
)

// noPlanText is what /plan show and celeste plan say without a plan.
const noPlanText = "No plan yet."

// legacyPlanNote heads a 1.x .celeste/plan.md shown for lack of a plan.json.
const legacyPlanNote = "Note: this is a 1.x .celeste/plan.md; 2.0 plans live in .celeste/plan.json (enter plan mode with /plan in the chat)."

// planReport renders workspace's approved plan with each step's todo
// status (ruling 12): [x] done, [>] in progress, [ ] pending, [-] the todo
// item was removed. Without a plan.json, a legacy .celeste/plan.md is shown
// with a note; without either, noPlanText.
func planReport(workspace string) string {
	plan, err := builtin.LoadPlan(workspace)
	if errors.Is(err, fs.ErrNotExist) {
		if data, lerr := os.ReadFile(filepath.Join(workspace, ".celeste", "plan.md")); lerr == nil {
			return legacyPlanNote + "\n\n" + strings.TrimRight(string(data), "\n")
		}
		return noPlanText
	}
	if err != nil {
		return "Could not read the plan: " + err.Error()
	}
	status := map[int]string{}
	for _, item := range builtin.NewTodoStore(workspace).List() {
		status[item.ID] = item.Status
	}
	var b strings.Builder
	if plan.Goal != "" {
		b.WriteString("Plan: " + plan.Goal)
	} else {
		b.WriteString("Plan")
	}
	if !plan.ApprovedAt.IsZero() {
		b.WriteString(" (approved " + plan.ApprovedAt.Local().Format("2006-01-02 15:04") + ")")
	}
	b.WriteString("\n")
	for i, s := range plan.Steps {
		mark := "[-]"
		st, ok := status[s.TodoID]
		switch {
		case !ok:
		case st == "done":
			mark = "[x]"
		case st == "in_progress":
			mark = "[>]"
		default:
			mark = "[ ]"
		}
		fmt.Fprintf(&b, "%s %d. %s", mark, i+1, s.Title)
		if !ok {
			b.WriteString(" (todo removed)")
		}
		b.WriteString("\n")
		if s.Detail != "" {
			b.WriteString("      " + s.Detail + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ShowPlan renders the chat workspace's plan (tui.PlanModer).
func (a *TUIClientAdapter) ShowPlan() string {
	ws := a.workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	return planReport(ws)
}
