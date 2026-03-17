package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestRouterResolvesConfiguredLane(t *testing.T) {
	cfg := &config.Config{
		Model: "default-model",
		Orchestrator: &config.OrchestratorConfig{
			Lanes: map[string]config.LaneConfig{
				"code": {Primary: "grok-fast", Reviewer: "gemini-review"},
			},
		},
	}
	r := orchestrator.NewRouter(cfg)
	assignment, err := r.Resolve(orchestrator.LaneCode)
	require.NoError(t, err)
	assert.Equal(t, "grok-fast", assignment.Primary)
	assert.Equal(t, "gemini-review", assignment.Reviewer)
	assert.True(t, assignment.HasReviewer())
}

func TestRouterFallsBackToDefaultModel(t *testing.T) {
	cfg := &config.Config{Model: "my-default"}
	r := orchestrator.NewRouter(cfg)
	assignment, err := r.Resolve(orchestrator.LaneContent)
	require.NoError(t, err)
	assert.Equal(t, "my-default", assignment.Primary)
	assert.False(t, assignment.HasReviewer())
}

func TestRouterBlankReviewerMeansNoDebate(t *testing.T) {
	cfg := &config.Config{
		Model: "primary",
		Orchestrator: &config.OrchestratorConfig{
			Lanes: map[string]config.LaneConfig{
				"code": {Primary: "primary", Reviewer: ""},
			},
		},
	}
	r := orchestrator.NewRouter(cfg)
	assignment, _ := r.Resolve(orchestrator.LaneCode)
	assert.False(t, assignment.HasReviewer())
}
