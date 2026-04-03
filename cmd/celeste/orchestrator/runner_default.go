package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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
	if cwd, err := os.Getwd(); err == nil {
		opts.Workspace = cwd
	}

	// Forward agent progress events into the orchestrator event stream.
	if r.onEvent != nil {
		emit := r.onEvent
		// turnStatsMap holds per-turn stats from OnTurnStats until a progress event consumes them.
		// turnStatsEmitted tracks whether stats have already been flushed for the current turn,
		// preventing double-emit when a turn has both tool calls and a completion response.
		// Both callbacks run on the same goroutine (agent runtime loop) — no mutex needed.
		//
		// Call order in runtime.go per turn:
		//   ProgressTurnStart → SendMessageSync → OnTurnStats → ProgressToolCall* → ProgressResponse?
		// OnTurnStats fires BEFORE ProgressToolCall, so we cannot use a "hadToolCall" flag in
		// OnTurnStats. Instead we flush stats from ProgressToolCall (first call per turn) and
		// from ProgressResponse (completion turns that never reach ProgressToolCall).
		turnStatsMap := make(map[int]agent.TurnStats)
		turnStatsEmitted := false

		opts.OnTurnStats = func(stats agent.TurnStats) {
			// Store stats; they will be flushed by ProgressToolCall or ProgressResponse.
			turnStatsMap[stats.Turn] = stats
		}

		opts.OnProgress = func(kind agent.ProgressKind, text string, turn, maxTurns int) {
			switch kind {
			case agent.ProgressTurnStart:
				turnStatsEmitted = false
				emit(OrchestratorEvent{Kind: EventAction, Model: r.model, Text: fmt.Sprintf("turn %d/%d", turn, maxTurns)})
			case agent.ProgressToolCall:
				// Flush per-turn stats on the first tool call of each turn so the user
				// sees timing/tokens before watching tool execution unfold.
				if !turnStatsEmitted {
					if stats, ok := turnStatsMap[turn]; ok {
						emit(OrchestratorEvent{
							Kind:         EventAction,
							Model:        r.model,
							Text:         fmt.Sprintf("↩ turn %d", turn),
							Duration:     stats.Elapsed,
							InputTokens:  stats.InputTokens,
							OutputTokens: stats.OutputTokens,
							Response:     stats.Response,
						})
						delete(turnStatsMap, turn)
						turnStatsEmitted = true
					}
				}
				emit(OrchestratorEvent{Kind: EventToolCall, Model: r.model, Text: fmt.Sprintf("⚙ %s", text)})
			case agent.ProgressResponse:
				preview := strings.TrimSpace(text)
				if nl := strings.IndexByte(preview, '\n'); nl > 0 {
					preview = preview[:nl]
				}
				if len(preview) > 120 {
					preview = preview[:120] + "…"
				}
				label := fmt.Sprintf("↩ turn %d", turn)
				if preview != "" {
					label = fmt.Sprintf("↩ turn %d: %s", turn, preview)
				}
				evt := OrchestratorEvent{Kind: EventAction, Model: r.model, Text: label, Response: text}
				if stats, ok := turnStatsMap[turn]; ok {
					evt.Duration = stats.Elapsed
					evt.InputTokens = stats.InputTokens
					evt.OutputTokens = stats.OutputTokens
					delete(turnStatsMap, turn)
				}
				emit(evt)
			}
		}
	}

	runner, err := agent.NewRunner(&cfg, opts, io.Discard, io.Discard)
	if err != nil {
		return "", err
	}
	defer runner.Close()
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
