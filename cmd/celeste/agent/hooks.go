package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/termsafe"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// compactTrigger is PreCompact/PostCompact's trigger for the agent's own
// compaction. Manual /compact is a TUI command.
const compactTrigger = "auto"

var errCompactionBlocked = errors.New("compaction blocked by a PreCompact hook")

// ErrGoalBlocked matches (errors.Is) a goal a UserPromptSubmit hook blocked:
// a deny, or a hook that failed. The error's text adds the hook's reason.
var ErrGoalBlocked = errors.New("goal blocked by a UserPromptSubmit hook")

// submitGoal runs the user's goal through UserPromptSubmit (Options.CheckGoal)
// and returns the text the model receives: the goal, plus the hooks' context
// in a <hook-context> block, the bytes the chat sends. The context is part of
// the goal message itself, so the planning request (which bypasses the loop)
// and a resumed run's checkpoint carry it too. An interrupted hook returns
// ctx's error.
func (r *Runner) submitGoal(ctx context.Context, goal string) (string, error) {
	if !r.options.CheckGoal {
		return goal, nil
	}
	msg, v, err := loop.UserPromptSubmit(hooks.WithWarn(ctx, r.warning), r.liveHooks(), tui.ChatMessage{Role: "user", Content: goal})
	if err != nil {
		return "", err
	}
	if v.Blocked {
		return "", fmt.Errorf("%w: %s", ErrGoalBlocked, v.Reason)
	}
	if c, _ := msg.Metadata[tui.MetaHookContext].(string); c != "" {
		goal += loop.HookContextBlock(c)
	}
	return goal, nil
}

// warning sends s to the run's warning sink. r.warn is already gated by
// NewRunner; the errOut fallback (runners built as struct literals) goes
// through the gate when there is one, so nothing is written after Close.
func (r *Runner) warning(s string) {
	if r.warn != nil {
		r.warn(s)
		return
	}
	write := func() { fmt.Fprintf(r.errOut, "Warning: %s\n", termsafe.Text(s)) }
	if r.gate != nil {
		r.gate.do(write)
		return
	}
	write()
}

// liveHooks returns the hooks runner, or nil when there is none or the
// runner is closed: after Close no hook fires on the caller's behalf, the
// same rule as the callbacks.
func (r *Runner) liveHooks() *hooks.Runner {
	if r.hooks == nil || (r.gate != nil && r.gate.isClosed()) {
		return nil
	}
	return r.hooks
}

// stopHook asks Stop hooks (top-level runs) or SubagentStop hooks (a nested
// runner with an AgentID) whether a finished run may end. It returns the
// instruction to continue with, or "" to finish. A deny is honoured once per
// run (*continued records it) and only while turns remain; later ones are
// reported and ignored, so a hook cannot keep the run going to the turn cap.
// Other nested runners (orchestrator lanes, /agent) fire neither.
func (r *Runner) stopHook(ctx context.Context, state *RunState, continued *bool) string {
	h := r.liveHooks()
	if h == nil {
		return ""
	}
	var out hooks.Outcome
	event := "Stop"
	switch {
	case !r.options.Nested:
		if !h.Has(hooks.EventStop) {
			return ""
		}
		out = h.Stop(ctx, state.LastAssistantResponse)
	case r.options.AgentID != "":
		if !h.Has(hooks.EventSubagentStop) {
			return ""
		}
		event = "SubagentStop"
		out = h.SubagentStop(ctx, r.options.AgentID, state.LastAssistantResponse)
	default:
		return ""
	}
	instr, warning := hooks.StopContinuation(out, *continued, state.Options.MaxTurns-state.Turn,
		hooks.StopScope{Event: event, Actor: "the agent", Unit: "run"})
	if warning != "" {
		r.warning(warning)
	}
	if instr != "" {
		*continued = true
	}
	return instr
}

// hookedSummarize wraps summarize with PreCompact the way
// TUIClientAdapter.SummarizeContext does: the hook runs inside the summarize
// call, which compact.Summarize makes only when there is something to
// summarize, so it never fires for pruning. It may block the summary or add
// instructions. blocked reports the hook's reason after a block.
func (r *Runner) hookedSummarize(summarize compact.SummarizeFunc) (compact.SummarizeFunc, func() string) {
	h := r.liveHooks()
	if h == nil || !h.Has(hooks.EventPreCompact) {
		return summarize, func() string { return "" }
	}
	blocked, fired := "", false
	return func(ctx context.Context, system, user string) (string, error) {
		if blocked != "" {
			return "", errCompactionBlocked
		}
		if !fired {
			fired = true
			pre := h.PreCompact(ctx, compactTrigger, "") // the agent's summary has no focus
			if pre.Decision != hooks.Allow {
				blocked = pre.Reason
				if blocked == "" {
					blocked = "no reason given"
				}
				return "", errCompactionBlocked
			}
			if pre.AdditionalContext != "" {
				user += "\n\nAdditional instructions from a PreCompact hook:\n" + pre.AdditionalContext
			}
		}
		return summarize(ctx, system, user)
	}, func() string { return blocked }
}

// postCompact fires PostCompact with the summary text.
func (r *Runner) postCompact(ctx context.Context, summary string) {
	if h := r.liveHooks(); h != nil {
		h.PostCompact(ctx, compactTrigger, summary)
	}
}
