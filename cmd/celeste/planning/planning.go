// Package planning implements plan mode for structured task execution.
// Plans are stored on disk so users can edit them in their IDE.
// The plan is read at exit time, not kept in memory.
package planning

import (
	"fmt"
	"os"
	"strings"
)

// PlanStep represents a single step in a plan.
type PlanStep struct {
	Description string `json:"description"`
	Status      string `json:"status"` // "pending", "in_progress", "done", "skipped"
}

// PlanMode manages the plan mode state machine.
type PlanMode struct {
	Active   bool   `json:"active"`
	PlanPath string `json:"plan_path"`
	Steps    []PlanStep
}

// NewPlanMode creates a new inactive PlanMode.
func NewPlanMode() *PlanMode {
	return &PlanMode{}
}

// Enter activates plan mode with the given file path.
func (pm *PlanMode) Enter(planPath string) {
	pm.Active = true
	pm.PlanPath = planPath
	pm.Steps = nil
}

// Exit reads the plan from disk, parses it, deactivates plan mode,
// and returns the final state.
func (pm *PlanMode) Exit() (*PlanMode, error) {
	if !pm.Active {
		return nil, fmt.Errorf("plan mode is not active")
	}

	content, err := pm.ReadPlan()
	if err != nil {
		// Still deactivate even if reading fails.
		pm.Active = false
		return nil, fmt.Errorf("failed to read plan on exit: %w", err)
	}

	pm.Steps = ParseSteps(content)
	pm.Active = false

	// Return a snapshot.
	snapshot := &PlanMode{
		Active:   false,
		PlanPath: pm.PlanPath,
		Steps:    make([]PlanStep, len(pm.Steps)),
	}
	copy(snapshot.Steps, pm.Steps)
	return snapshot, nil
}

// IsActive returns whether plan mode is currently active.
func (pm *PlanMode) IsActive() bool {
	return pm.Active
}

// WritePlan writes content to the plan file on disk.
func (pm *PlanMode) WritePlan(content string) error {
	if pm.PlanPath == "" {
		return fmt.Errorf("no plan path set")
	}
	return os.WriteFile(pm.PlanPath, []byte(content), 0644)
}

// ReadPlan reads the plan file from disk, allowing users to edit it externally.
func (pm *PlanMode) ReadPlan() (string, error) {
	if pm.PlanPath == "" {
		return "", fmt.Errorf("no plan path set")
	}
	data, err := os.ReadFile(pm.PlanPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseSteps parses markdown checklist lines into PlanSteps.
// Supports: - [ ] pending, - [x] done, - [~] skipped, - [>] in_progress
func ParseSteps(content string) []PlanStep {
	var steps []PlanStep
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if step, ok := parseChecklistLine(trimmed); ok {
			steps = append(steps, step)
		}
	}
	return steps
}

// parseChecklistLine parses a single checklist line like "- [ ] Do something".
func parseChecklistLine(line string) (PlanStep, bool) {
	prefixes := map[string]string{
		"- [ ] ": "pending",
		"- [x] ": "done",
		"- [X] ": "done",
		"- [~] ": "skipped",
		"- [>] ": "in_progress",
	}
	for prefix, status := range prefixes {
		if strings.HasPrefix(line, prefix) {
			desc := strings.TrimSpace(line[len(prefix):])
			if desc != "" {
				return PlanStep{Description: desc, Status: status}, true
			}
		}
	}
	return PlanStep{}, false
}
