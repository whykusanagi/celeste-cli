package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
)

// submitPrompt runs UserPromptSubmit hooks on the call's prompt, with the
// chat UI's rules: anything but allow (a failed hook included) refuses the
// call, and additionalContext is sent with the prompt in a <hook-context>
// block.
func submitPrompt(ctx context.Context, h *hooks.Runner, prompt string) (string, error) {
	if !h.Has(hooks.EventUserPromptSubmit) { // Has is nil-safe
		return prompt, nil
	}
	out := h.UserPromptSubmit(ctx, prompt)
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("chat error: %w", err) // an interrupt, not a verdict
	}
	if out.Decision != hooks.Allow {
		reason := out.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return "", fmt.Errorf("prompt blocked by a UserPromptSubmit hook: %s", reason)
	}
	if out.AdditionalContext != "" {
		prompt += "\n\n<hook-context>\n" + out.AdditionalContext + "\n</hook-context>"
	}
	return prompt, nil
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
