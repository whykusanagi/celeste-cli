package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestVerdictApprovedWhenNoIssues(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
	result := dm.Verdict([]orchestrator.Issue{})
	assert.Equal(t, orchestrator.VerdictApproved, result.Kind)
	assert.Greater(t, result.Score, 0.8)
}

func TestVerdictNeedsWorkWhenIssuesExist(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
	issues := []orchestrator.Issue{{File: "main.go", Line: 10, Severity: "high", Description: "nil dereference"}}
	result := dm.Verdict(issues)
	assert.Equal(t, orchestrator.VerdictNeedsWork, result.Kind)
	assert.Less(t, result.Score, 0.8)
}

func TestVerdictContestedAfterMaxRounds(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{MaxRounds: 2})
	for i := 0; i < 2; i++ {
		dm.AddTurn(orchestrator.DebateTurn{Round: i + 1, Role: orchestrator.RoleReviewer, Output: "still has issues"})
		dm.AddTurn(orchestrator.DebateTurn{Round: i + 1, Role: orchestrator.RolePrimary, Output: "disagree"})
	}
	issues := []orchestrator.Issue{{File: "main.go", Line: 1, Severity: "medium", Description: "unclear"}}
	result := dm.Verdict(issues)
	assert.Equal(t, orchestrator.VerdictContested, result.Kind)
}
