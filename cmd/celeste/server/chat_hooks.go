package server

import (
	"context"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
)

// promptBlockedError refuses an MCP chat call whose prompt a
// UserPromptSubmit hook blocked (a deny, or a hook that failed). Its text is
// the pre-loop server's, verbatim: the plugin sees it in the tool result.
type promptBlockedError struct{ reason string }

func (e *promptBlockedError) Error() string {
	return "prompt blocked by a UserPromptSubmit hook: " + e.reason
}

// chatStopHook asks Stop hooks whether a finished call may end, and returns
// the instruction to continue with, or "" to finish. This is MCP agent
// mode's rule (hooks.StopContinuation): a deny is honoured once per call
// and only while turns remain; later ones are reported and ignored. The
// instruction is trimmed, as MCP chat always sent it.
func chatStopHook(ctx context.Context, h *hooks.Runner, final string, continued bool, turnsLeft int, warn func(string)) string {
	if !h.Has(hooks.EventStop) {
		return ""
	}
	instr, warning := hooks.StopContinuation(h.Stop(ctx, final), continued, turnsLeft,
		hooks.StopScope{Event: "Stop", Actor: "the chat", Unit: "call"})
	if warning != "" {
		warn(warning)
	}
	return strings.TrimSpace(instr)
}
