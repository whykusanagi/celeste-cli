package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/steer"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// gateVetoPrompt continues a run whose completion the ballot rejected.
const gateVetoPrompt = "The watchdog found no tool result that backs your claim of success. Run the check that proves the work is done (the build, the tests, or reading the output back), then finish with '%s' and what the check showed."

// completion is the completion gate (spec §5 W3). The gate accepts a
// reply whose first or last non-empty line starts with the marker (the
// agent prompt asks for it first; the spec names the final line, and both
// anchor it, unlike a substring), unless the ballot, asked now, reports
// claims_unverified_success; it vetoes at most once per run. In shadow
// mode (the default) the substring check decides, as before, and a
// disagreement is logged. It returns whether the run may complete, and
// whether the gate vetoed (it has then appended the continue prompt). A
// run with verification commands is left to the runtime's check: no
// ballot veto.
func (r *Runner) completion(ctx context.Context, state *RunState, final string, s *steer.Session) (complete, vetoed bool) {
	old := isCompletionResponse(state.LastAssistantResponse, state.Options)
	anchored := markerOnLine(final, state.Options)
	gate := anchored
	// With verification commands the runtime checks the work after the
	// marker (as the task-complete-before-verify rule stands down): no
	// ballot veto on top.
	verifies := state.Options.RequireVerification && len(state.Options.VerificationCommands) > 0
	if anchored && state.GateVetoes == 0 && !verifies {
		if v, acting := s.Ballot(ctx); acting && v.Has(steer.QUnverified) {
			gate, vetoed = false, true
		}
	}
	if r.gateMode != config.ModeOn {
		if gate != old {
			fmt.Fprintf(r.errOut, "[agent] completion gate (shadow): would %s this reply (anchored marker: %v)\n", map[bool]string{true: "accept", false: "reject"}[gate], anchored)
		}
		return old, false
	}
	if vetoed {
		// The veto prompt says it: a watchdog concern about the same
		// finding is not given as well.
		s.Settle(steer.QUnverified)
		state.GateVetoes++
		state.Messages = append(state.Messages, tui.ChatMessage{
			Role: "user", Content: fmt.Sprintf(gateVetoPrompt, state.Options.CompletionMarker), Timestamp: time.Now(),
		})
		state.Steps = append(state.Steps, Step{Turn: state.Turn, Type: "completion_vetoed", Timestamp: time.Now()})
	}
	return gate, vetoed
}

// markerOnLine reports the completion marker at the start of the reply's
// first or last non-empty line, after markdown decoration (*, _, #, >, `).
// Without RequireCompletionMarker any non-empty reply completes, as before.
func markerOnLine(text string, o Options) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if !o.RequireCompletionMarker {
		return true
	}
	marker := strings.ToUpper(strings.TrimRight(strings.TrimSpace(o.CompletionMarker), ":"))
	if marker == "" {
		return true
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimLeft(strings.TrimSpace(l), "*_#>` "); l != "" {
			lines = append(lines, strings.ToUpper(l))
		}
	}
	if len(lines) == 0 {
		return false
	}
	return startsWithMarker(lines[0], marker) || startsWithMarker(lines[len(lines)-1], marker)
}

// startsWithMarker: line begins with marker as a whole token, so
// TASK_COMPLETED or TASK_COMPLETE_LATER do not count.
func startsWithMarker(line, marker string) bool {
	if !strings.HasPrefix(line, marker) {
		return false
	}
	rest := line[len(marker):]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}
