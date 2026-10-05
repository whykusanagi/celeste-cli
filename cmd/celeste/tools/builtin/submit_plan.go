package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// SubmitPlanName is the plan-mode tool's name; the chat's plan-mode filter
// lets it through alongside the read-only tools.
const SubmitPlanName = "submit_plan"

// Plan-mode limits and texts (2.0 W4e rulings 8 and 10).
const (
	maxPlanSteps = 30

	planApprove = "Approve and start"
	planKeep    = "Keep planning"

	planKeepResult = "The user wants changes to the plan; keep planning (read-only) and submit again."
	planHeadless   = "plan mode needs the interactive chat"

	// answerTimeout bounds a tool that waits on the user in a modal
	// (submit_plan's approval, ask's question): reading a plan or
	// weighing a choice takes longer than the default tool timeout. Esc
	// or the end of the turn still cancels it sooner.
	answerTimeout = 30 * time.Minute
)

// PlanFile is .celeste/plan.json: the approved plan.
type PlanFile struct {
	Goal       string     `json:"goal"`
	Steps      []PlanStep `json:"steps"`
	ApprovedAt time.Time  `json:"approved_at"`
}

// PlanStep is one step of an approved plan, with the todo item it became.
type PlanStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	TodoID int    `json:"todo_id"`
}

// PlanPath is where a workspace's approved plan lives.
func PlanPath(workspace string) string {
	return filepath.Join(workspace, ".celeste", "plan.json")
}

// LoadPlan reads the workspace's approved plan. A missing file is an error
// satisfying errors.Is(err, fs.ErrNotExist).
func LoadPlan(workspace string) (*PlanFile, error) {
	data, err := os.ReadFile(PlanPath(workspace))
	if err != nil {
		return nil, err
	}
	var p PlanFile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(".celeste", "plan.json"), err)
	}
	return &p, nil
}

// SubmitPlanTool is the chat's plan-mode exit: the model submits ordered
// steps, the user approves them in the ask modal, and the approved plan is
// written to .celeste/plan.json with one todo item per step. It is
// registered only on the chat's registry.
//
// It reports read-only: it changes nothing until the user approves in its
// own modal, so it needs no permission prompt in front of that one.
type SubmitPlanTool struct {
	BaseTool
	workspace string
	ask       func(context.Context, tools.AskRequest) (tools.AskResponse, error)
	approved  func(PlanFile)
	// DefaultGoal, when set, supplies the goal a submission leaves out
	// (the chat's /plan <goal>).
	DefaultGoal func() string
	// Todos is the store approved steps go to: the registry's todo tool's
	// store when there is one, so the tool never overwrites them from a
	// stale copy. Nil opens the workspace's list.
	Todos *TodoStore
}

// NewSubmitPlanTool builds submit_plan for workspace. ask presents the
// approval (nil or failing: headless, an error); approved runs after an
// approved plan is saved, with that plan (the chat leaves plan mode).
func NewSubmitPlanTool(workspace string, ask func(context.Context, tools.AskRequest) (tools.AskResponse, error), approved func(PlanFile)) *SubmitPlanTool {
	return &SubmitPlanTool{
		BaseTool: BaseTool{
			ToolName: SubmitPlanName,
			ToolDescription: "Submit your plan for the user's approval (plan mode). Give concrete, ordered steps. " +
				"If the user approves, the steps become todo items, plan mode ends and you start with step 1; " +
				"otherwise keep investigating with read-only tools and submit a revised plan.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"goal": {"type": "string", "description": "What the plan achieves (optional)."},
					"steps": {
						"type": "array",
						"minItems": 1,
						"maxItems": 30,
						"description": "The ordered steps (1-30).",
						"items": {
							"type": "object",
							"properties": {
								"title": {"type": "string", "description": "A short, concrete step."},
								"detail": {"type": "string", "description": "Optional detail: files, approach, checks."}
							},
							"required": ["title"]
						}
					}
				},
				"required": ["steps"]
			}`),
			ReadOnly:       true,
			Interrupt:      tools.InterruptCancel,
			RequiredFields: []string{"steps"},
			ExecTimeout:    answerTimeout,
		},
		workspace: workspace,
		ask:       ask,
		approved:  approved,
	}
}

// parsePlanInput validates the model's goal and steps.
func parsePlanInput(input map[string]any) (string, []PlanStep, error) {
	goal := ""
	if raw, ok := input["goal"]; ok && raw != nil {
		s, ok := raw.(string)
		if !ok {
			return "", nil, fmt.Errorf("goal must be a string")
		}
		goal = strings.TrimSpace(s)
	}
	raw, _ := input["steps"].([]any)
	if len(raw) == 0 {
		return "", nil, fmt.Errorf("steps must be a list of 1-%d objects {title, detail?}", maxPlanSteps)
	}
	if len(raw) > maxPlanSteps {
		return "", nil, fmt.Errorf("too many steps: %d (at most %d); merge smaller steps", len(raw), maxPlanSteps)
	}
	steps := make([]PlanStep, 0, len(raw))
	for i, r := range raw {
		obj, ok := r.(map[string]any)
		if !ok {
			return "", nil, fmt.Errorf("steps[%d] must be an object {title, detail?}", i)
		}
		title, _ := obj["title"].(string)
		title = strings.TrimSpace(title)
		if title == "" {
			return "", nil, fmt.Errorf("steps[%d].title must be a non-empty string", i)
		}
		detail := ""
		if d, ok := obj["detail"]; ok && d != nil {
			s, ok := d.(string)
			if !ok {
				return "", nil, fmt.Errorf("steps[%d].detail must be a string", i)
			}
			detail = strings.TrimSpace(s)
		}
		steps = append(steps, PlanStep{Title: title, Detail: detail})
	}
	return goal, steps, nil
}

// planQuestion is the approval modal's question: the goal and the numbered
// steps.
func planQuestion(goal string, steps []PlanStep) string {
	var b strings.Builder
	b.WriteString("Approve this plan?\n\n")
	if goal != "" {
		b.WriteString("Goal: " + goal + "\n\n")
	}
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Title)
		if s.Detail != "" {
			b.WriteString("   " + s.Detail + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Execute validates the plan, asks the user, and on approval saves it and
// creates the todo items.
func (t *SubmitPlanTool) Execute(ctx context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	goal, steps, err := parsePlanInput(input)
	if err != nil {
		return tools.ToolResult{Content: err.Error(), Error: true}, nil
	}
	if goal == "" && t.DefaultGoal != nil {
		goal = strings.TrimSpace(t.DefaultGoal())
	}
	if t.ask == nil {
		return tools.ToolResult{Content: planHeadless, Error: true}, nil
	}
	resp, err := t.ask(ctx, tools.AskRequest{
		Question: planQuestion(goal, steps),
		Options: []tools.AskOption{
			// Keep planning first: the modal pre-selects the first option,
			// so Enter (or typed text and Enter) never approves by default.
			{Label: planKeep, Description: "Stay in plan mode; tell Celeste what to change."},
			{Label: planApprove, Description: "Save the plan, add its steps to the todo list and leave plan mode."},
		},
	})
	if err != nil && ctx.Err() == nil {
		return tools.ToolResult{Content: planHeadless, Error: true}, nil
	}
	if err != nil || resp.Cancelled || len(resp.Selected) == 0 || resp.Selected[0] != planApprove {
		return tools.ToolResult{Content: planKeepResult}, nil
	}

	store := t.Todos
	if store == nil {
		store = NewTodoStore(t.workspace)
	}
	for i := range steps {
		steps[i].TodoID = store.Create(steps[i].Title, steps[i].Detail).ID
	}
	plan := PlanFile{Goal: goal, Steps: steps, ApprovedAt: time.Now().UTC()}
	data, _ := json.MarshalIndent(plan, "", "  ")
	path := PlanPath(t.workspace)
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err == nil {
		err = atomicfile.Write(path, append(data, '\n'), 0o644)
	}
	if err != nil {
		// No plan, no todo items: take back the ones just created.
		for _, s := range steps {
			_ = store.Delete(s.TodoID)
		}
		return tools.ToolResult{Content: "could not save the plan: " + err.Error(), Error: true}, nil
	}
	if t.approved != nil {
		t.approved(plan)
	}
	// "Plan approved:" also tells the chat a resumed session's plan was
	// approved (tui.planApprovedPrefix).
	return tools.ToolResult{Content: approvedResult(steps)}, nil
}

// approvedResult is submit_plan's result for an approved plan: the steps
// with the todo id each became, and how to tick them (#325: without the
// ids and the exact call, models carried out the plan and never updated
// the list).
func approvedResult(steps []PlanStep) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan approved: %d todo items created. Plan mode is off; start with step 1.\n\n", len(steps))
	b.WriteString("Each step is a todo item. Keep its status current with the todo tool as you work:\n")
	b.WriteString(`- when you start a step: todo {"action":"update","id":<id>,"status":"in_progress"}` + "\n")
	b.WriteString(`- as soon as it is finished, before the next step: todo {"action":"update","id":<id>,"status":"done"}` + "\n\n")
	b.WriteString("Steps:\n")
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s (todo id %d)\n", i+1, s.Title, s.TodoID)
	}
	return strings.TrimRight(b.String(), "\n")
}
