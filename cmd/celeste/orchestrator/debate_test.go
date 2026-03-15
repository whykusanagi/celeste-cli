package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestDebateManagerDefaultsToThreeRounds(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
	assert.Equal(t, 3, dm.MaxRounds())
}

func TestDebateManagerCustomRounds(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{MaxRounds: 5})
	assert.Equal(t, 5, dm.MaxRounds())
}

func TestAddTurnAppendsTurn(t *testing.T) {
	dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
	dm.AddTurn(orchestrator.DebateTurn{Round: 1, Role: orchestrator.RoleReviewer, Output: "issue: nil check missing"})
	dm.AddTurn(orchestrator.DebateTurn{Round: 1, Role: orchestrator.RolePrimary, Output: "accepted"})
	require.Len(t, dm.Turns(), 2)
	assert.Equal(t, orchestrator.RoleReviewer, dm.Turns()[0].Role)
}

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
