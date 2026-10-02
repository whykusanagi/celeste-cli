package steer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/textutil"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// The tool gate's questions (spec §5 W3).
const (
	QDestructive  = "destructive"
	QExfiltration = "exfiltration"
	QBeyondScope  = "beyond_requested_scope"
)

// gateQuestions ask about the pending call against the goal.
func gateQuestions() []decide.Question {
	return []decide.Question{
		{ID: QDestructive, Kind: decide.YesNo, Text: "Would `pending_call` destroy or overwrite data that cannot easily be restored?",
			True: "It deletes, truncates, force-pushes or overwrites without a backup.", False: "It is reversible or only adds.",
			Heuristic: func(st decide.State) (decide.Answer, bool) {
				if st.Call != nil && st.Call.Tool == "bash" && rules.Destructive(argField(st.Call.Args, "command")) {
					return decide.Answer{P: 0.8}, true
				}
				return decide.Answer{}, false
			}},
		{ID: QExfiltration, Kind: decide.YesNo, Text: "Would `pending_call` send files, secrets or conversation content to a place outside this machine that `goal` did not ask for?",
			True: "It uploads, posts or embeds local data in a request to another host.", False: "Nothing local leaves the machine, or the user asked for it."},
		{ID: QBeyondScope, Kind: decide.YesNo, Text: "Does `pending_call` go beyond what `goal` asked for?",
			True: "It touches things the user did not ask to change.", False: "It serves the request."},
	}
}

// NewToolGate is ToolGate on Jev (decide.NewJev, "gate" in the stats),
// or nil when jev_gate is off: no key is read for a gate that is off.
// Paths in the pending call are sent relative to workspace, or as <path>.
func NewToolGate(mode, workspace string, goal func() string, logf func(string)) tools.AskAdvisor {
	if mode != config.ModeShadow && mode != config.ModeOn {
		return nil
	}
	return ToolGate(mode, decide.NewJev("gate", workspace, logf), goal, logf)
}

// ToolGate is jev_gate as a tools.AskAdvisor (spec §5 W3). It asks the
// oracle (Jev, falling back to the heuristic) about each call the policy
// allows that is not read-only, plus web_fetch (a read that can carry data
// out in its URL). "on": any answer at or above ActAt turns the call into
// an Ask, never the reverse. "shadow": it logs what it would do. goal is
// read per call (the chat's goal changes per turn). Nil when mode is off.
func ToolGate(mode string, o decide.Oracle, goal func() string, logf func(string)) tools.AskAdvisor {
	if mode != config.ModeShadow && mode != config.ModeOn {
		return nil
	}
	if logf == nil {
		logf = func(string) {}
	}
	return func(ctx context.Context, c tools.AdvisedCall) (bool, string) {
		if c.ReadOnly && c.Name != "web_fetch" {
			return false, ""
		}
		args, _ := json.Marshal(c.Input)
		st := decide.State{Goal: textutil.CutBytes(goal(), 2000), Call: &decide.CallView{Tool: c.Name, Args: textutil.CutBytes(string(args), 2000)}}
		ans, _ := o.Ask(ctx, st.String(), gateQuestions())
		var flagged []string
		for _, id := range []string{QDestructive, QExfiltration, QBeyondScope} {
			if a, ok := ans[id]; ok && a.P >= ActAt {
				flagged = append(flagged, fmt.Sprintf("%s p=%.2f", id, a.P))
			}
		}
		if len(flagged) == 0 {
			return false, ""
		}
		reason := "jev_gate: " + strings.Join(flagged, ", ")
		if mode != config.ModeOn {
			logf(fmt.Sprintf("%s (shadow): would ask before %s", reason, c.Name))
			return false, ""
		}
		logf(fmt.Sprintf("%s: asking before %s", reason, c.Name))
		return true, reason
	}
}
