package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/steer"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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
// Progress-marker lines before it (STEP_DONE: 1, as the agent prompt asks
// for) do not count as the first line (#330), and reasoning a server left
// in the reply, closed by a </think> whose opening tag the chat template
// sent (qwen3 without a reasoning parser), is not judged: only the reply
// after it. Reasoning with nothing after its </think> (a stream cut off
// by max_tokens) has no reply and never completes. Without
// RequireCompletionMarker any non-empty reply completes, as before.
func markerOnLine(text string, o Options) bool {
	if reply, ok := afterLeakedThink(text); ok {
		return markerIn(reply, o)
	}
	return markerIn(text, o)
}

// markerIn is markerOnLine for a reply with no leaked reasoning.
func markerIn(text string, o Options) bool {
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
	if startsWithMarker(lines[len(lines)-1], marker) {
		return true
	}
	for _, l := range lines {
		if startsWithMarker(l, marker) {
			return true
		}
		if !progressMarker.MatchString(l) {
			return false
		}
	}
	return false
}

// progressMarker is an upper-snake token and a colon at the start of an
// (upper-cased) line: STEP_DONE: 1, PLAN_STEP: 2. TASK_COMPLETED: and the
// like match too, so a near-miss of the marker is skipped, never taken.
var progressMarker = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+\s*:`)

// afterLeakedThink returns the text after a </think> line that no <think>
// opened: the reasoning of a model whose chat template sent the opening
// tag, left in the reply by a server without a reasoning parser. A leading
// <think> block is stripped by the backend already, and a </think> inside
// a line is prose about the tag.
func afterLeakedThink(text string) (string, bool) {
	loc := leakedThinkClose.FindStringIndex(text)
	if loc == nil || strings.Contains(text[:loc[0]], "<think>") {
		return "", false
	}
	return text[loc[1]:], true
}

var leakedThinkClose = regexp.MustCompile(`(?m)^[ \t]*</think>[ \t]*$`)

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
