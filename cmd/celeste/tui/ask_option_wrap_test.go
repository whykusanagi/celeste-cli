package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A plan-modal option whose description is too long for the row wraps
// under its own text, never back to column 0 ("plan mode." at 80), at both
// audit sizes.
func TestAskOptionDescriptionHangs(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m, _ = step(t, m, AskRequestMsg{
				Question: "Approve this plan?\n\nGoal: Write plan_out.txt\n\n1. Write plan_out.txt\n   one line",
				Options: []AskOption{
					{Label: "Keep planning", Description: "Stay in plan mode; tell Celeste what to change."},
					{Label: "Approve and start", Description: "Save the plan, add its steps to the todo list and leave plan mode, then start on the first step right away."},
				},
				Response: make(chan AskResponseMsg, 1),
			})
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			rows := strings.Split(frame, "\n")
			start := -1
			for i, r := range rows {
				if strings.Contains(r, "Approve and start") {
					start = i
				}
			}
			if !assert.GreaterOrEqual(t, start, 0, frame) {
				return
			}
			col := strings.Index(rows[start], "Save the plan")
			assert.Positive(t, col, rows[start])
			for _, r := range rows[start+1:] {
				if strings.HasPrefix(r, "↑/↓") || strings.TrimSpace(r) == "" {
					break
				}
				assert.True(t, strings.HasPrefix(r, "  "), "option text wrapped to column 0: %q", r)
			}
			flat := strings.Join(strings.Fields(frame), " ")
			assert.Contains(t, flat, "then start on the first step right away.", frame)
		})
	}
}
