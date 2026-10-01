package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
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

// Each lane's models resolve against the lane's own endpoint.
func TestRouterResolvesLaneModelsOnTheirEndpoints(t *testing.T) {
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu"}, {ID: "fugu-ultra"}})()
	cfg := &config.Config{
		BaseURL: "https://api.sakana.ai/v1", Model: "fugu",
		Orchestrator: &config.OrchestratorConfig{Lanes: map[string]config.LaneConfig{
			"code": {Primary: "venice-uncensored", PrimaryBaseURL: "https://api.venice.ai/api/v1", Reviewer: "fugu-ultra"},
		}},
	}
	a, err := orchestrator.NewRouter(cfg).Resolve(orchestrator.LaneCode)
	require.NoError(t, err)
	assert.Equal(t, "venice-uncensored-1-2", a.Primary, "the primary resolves on Venice")
	assert.Equal(t, "fugu-ultra", a.Reviewer, "the reviewer is served on the default endpoint")
	assert.Len(t, a.Notes, 1)

	cfg.PinModel = true
	a, _ = orchestrator.NewRouter(cfg).Resolve(orchestrator.LaneCode)
	assert.Equal(t, "venice-uncensored", a.Primary, "pin_model turns resolution off")
}
