package orchestrator

import (
	"context"
	"io"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

type realAgentRunner struct {
	cfg   *config.Config
	model string
}

func (r *realAgentRunner) RunGoal(ctx context.Context, goal string) (string, error) {
	cfg := *r.cfg
	cfg.Model = r.model
	opts := agent.DefaultOptions()
	runner, err := agent.NewRunner(&cfg, opts, io.Discard, io.Discard)
	if err != nil {
		return "", err
	}
	state, err := runner.RunGoal(ctx, goal)
	if state != nil {
		return state.LastAssistantResponse, err
	}
	return "", err
}

func defaultRunnerFactory(cfg *config.Config) RunnerFactory {
	return func(model string) AgentRunner {
		return &realAgentRunner{cfg: cfg, model: model}
	}
}
