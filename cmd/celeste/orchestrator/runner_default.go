package orchestrator

import (
	"context"
	"fmt"
	"io"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

type realAgentRunner struct {
	cfg     *config.Config
	model   string
	onEvent func(OrchestratorEvent)
}

func (r *realAgentRunner) RunGoal(ctx context.Context, goal string) (string, error) {
	cfg := *r.cfg
	cfg.Model = r.model
	opts := agent.DefaultOptions()

	// Forward agent progress events into the orchestrator event stream so the
	// TUI action feed shows tool calls and turn progress in real time.
	if r.onEvent != nil {
		emit := r.onEvent
		opts.OnProgress = func(kind agent.ProgressKind, text string, turn, maxTurns int) {
			switch kind {
			case agent.ProgressTurnStart:
				emit(OrchestratorEvent{Kind: EventAction, Text: fmt.Sprintf("turn %d/%d", turn, maxTurns)})
			case agent.ProgressToolCall:
				emit(OrchestratorEvent{Kind: EventToolCall, Text: fmt.Sprintf("⚙ %s", text)})
			case agent.ProgressResponse:
				emit(OrchestratorEvent{Kind: EventAction, Text: fmt.Sprintf("↩ response (turn %d)", turn)})
			}
		}
	}

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

// defaultRunnerFactory creates a RunnerFactory that forwards agent progress
// events to the orchestrator via the provided onEvent callback.
func defaultRunnerFactory(cfg *config.Config, onEvent func(OrchestratorEvent)) RunnerFactory {
	return func(model string) AgentRunner {
		return &realAgentRunner{cfg: cfg, model: model, onEvent: onEvent}
	}
}
