package hooks

import (
	"fmt"
	"strings"
)

// StopScope names, for StopContinuation's warnings, the event that fired
// ("Stop" or "SubagentStop"), who it asked to continue ("the chat", "the
// agent") and the unit a continuation is counted per ("turn", "call", "run").
type StopScope struct {
	Event, Actor, Unit string
}

// StopContinuation is the rule the chat, MCP chat and the agent share for a
// Stop or SubagentStop outcome: a deny continues the work once per unit and
// only while turns remain; a later deny, or one with no turns left, is
// reported as a warning and ignored, so a hook cannot keep the work going to
// the turn cap. instr is what to continue with (the hook's reason, or
// "Continue." when it gave none), or "" to finish; warning is "" unless a
// deny was ignored.
func StopContinuation(out Outcome, continued bool, turnsLeft int, s StopScope) (instr, warning string) {
	if out.Decision != Deny {
		return "", ""
	}
	switch {
	case continued:
		return "", fmt.Sprintf("a %s hook asked %s to continue again; ignored (one continuation per %s)", s.Event, s.Actor, s.Unit)
	case turnsLeft <= 0:
		return "", fmt.Sprintf("a %s hook asked %s to continue, but the %s has no turns left", s.Event, s.Actor, s.Unit)
	}
	if strings.TrimSpace(out.Reason) == "" {
		return "Continue.", ""
	}
	return out.Reason, ""
}
