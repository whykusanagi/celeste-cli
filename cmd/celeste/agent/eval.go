package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type EvalCase struct {
	Name           string   `json:"name"`
	Goal           string   `json:"goal"`
	MaxTurns       int      `json:"max_turns,omitempty"`
	MustContain    []string `json:"must_contain,omitempty"`
	MustNotContain []string `json:"must_not_contain,omitempty"`
}

type EvalSuite struct {
	Cases []EvalCase `json:"cases"`
}

type EvalResult struct {
	CaseName string
	RunID    string
	Status   string
	Passed   bool
	Reason   string
}

func LoadEvalCases(path string) ([]EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var suite EvalSuite
	if err := json.Unmarshal(data, &suite); err == nil && len(suite.Cases) > 0 {
		return suite.Cases, nil
	}

	var direct []EvalCase
	if err := json.Unmarshal(data, &direct); err != nil {
		return nil, fmt.Errorf("parse eval file: %w", err)
	}
	return direct, nil
}

func (r *Runner) RunEval(ctx context.Context, cases []EvalCase) ([]EvalResult, error) {
	results := make([]EvalResult, 0, len(cases))
	for _, c := range cases {
		if strings.TrimSpace(c.Goal) == "" {
			results = append(results, EvalResult{
				CaseName: c.Name,
				Status:   StatusFailed,
				Passed:   false,
				Reason:   "empty goal",
			})
			continue
		}

		caseRunner := *r
		caseOptions := r.options
		caseOptions.DisableCheckpoints = true
		if c.MaxTurns > 0 {
			caseOptions.MaxTurns = c.MaxTurns
		}
		caseRunner.options = caseOptions

		state, err := caseRunner.RunGoal(ctx, c.Goal)
		if err != nil {
			results = append(results, EvalResult{
				CaseName: safeCaseName(c),
				RunID:    stateID(state),
				Status:   StatusFailed,
				Passed:   false,
				Reason:   err.Error(),
			})
			continue
		}

		finalText := strings.TrimSpace(state.LastAssistantResponse)
		passed, reason := evaluateCase(c, state.Status, finalText)
		results = append(results, EvalResult{
			CaseName: safeCaseName(c),
			RunID:    state.RunID,
			Status:   state.Status,
			Passed:   passed,
			Reason:   reason,
		})
	}

	return results, nil
}

func evaluateCase(c EvalCase, status, finalText string) (bool, string) {
	if status != StatusCompleted {
		return false, fmt.Sprintf("status=%s", status)
	}
	for _, required := range c.MustContain {
		if required == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(finalText), strings.ToLower(required)) {
			return false, fmt.Sprintf("missing required text: %q", required)
		}
	}
	for _, banned := range c.MustNotContain {
		if banned == "" {
			continue
		}
		if strings.Contains(strings.ToLower(finalText), strings.ToLower(banned)) {
			return false, fmt.Sprintf("contains forbidden text: %q", banned)
		}
	}
	return true, "ok"
}

func safeCaseName(c EvalCase) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return c.Goal
}

func stateID(state *RunState) string {
	if state == nil {
		return ""
	}
	return state.RunID
}
