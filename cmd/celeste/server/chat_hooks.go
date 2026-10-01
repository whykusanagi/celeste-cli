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
// mode's rule (agent/hooks.go stopHook): a deny is honoured once per call
// and only while turns remain; later ones are reported and ignored.
func chatStopHook(ctx context.Context, h *hooks.Runner, final string, continued bool, turnsLeft int, warn func(string)) string {
	if !h.Has(hooks.EventStop) {
		return ""
	}
	out := h.Stop(ctx, final)
	if out.Decision != hooks.Deny {
		return ""
	}
	switch {
	case continued:
		warn("a Stop hook asked the chat to continue again; ignored (one continuation per call)")
		return ""
	case turnsLeft <= 0:
		warn("a Stop hook asked the chat to continue, but the call has no turns left")
		return ""
	}
	if r := strings.TrimSpace(out.Reason); r != "" {
		return r
	}
	return "Continue."
}
