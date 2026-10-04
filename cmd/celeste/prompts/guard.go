package prompts

import (
	"fmt"
	"slices"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// guardDivisor: a persona profile may take at most a quarter of the
// resolved context window (spec W5 "Small-window guard").
const guardDivisor = 4

// offDivisor: lite, the smallest profile with a voice, stays while it fits
// in half the window, even over a quarter (W5 ruling 1: the built lite is
// ~3k tokens, and the spec gives chat lite at 8,192). In a window smaller
// than that the persona falls to off: identity, the honesty rule and the
// voice boundary (personaStatic), never nothing.
const offDivisor = 2

// stepDown is the order the guard walks, one level at a time. Off is the
// floor; a prompt asked for at off is never stepped.
var stepDown = []Profile{ProfileFull, ProfileSpine, ProfileLite}

// selectProfile returns the profile to use for want in a window of window
// tokens (0 or less = unknown: no guard). It steps full → spine → lite while
// the profile is over a quarter of the window, then to off if lite is over
// half of it. When it stepped, the second result is the guard's notice:
// returned once per (want, chosen, window) per process (ruling 7). The
// guard does not log it: the caller shows it, or logs it when it has
// nowhere to show it, so each run reports it once (#321). Sizes are the
// container's estimate, len(system_prompt)/4.
func selectProfile(want Profile, window int) (*PersonaProfile, string) {
	pp := mustProfile(want)
	i := slices.Index(stepDown, want)
	if window <= 0 || i < 0 {
		return pp, ""
	}
	for i < len(stepDown)-1 && pp.Tokens() > window/guardDivisor {
		i++
		pp = mustProfile(stepDown[i])
	}
	if pp.Profile == ProfileLite && pp.Tokens() > window/offDivisor {
		pp = mustProfile(ProfileOff)
	}
	if pp.Profile == want {
		return pp, ""
	}
	return pp, guardNotice(want, pp.Profile, window)
}

// NoticePrefix starts every surface's line for the guard's notice, so the
// TUI, `celeste message` and `celeste agent` show it the same way.
const NoticePrefix = "ℹ "

// guardSeen holds the (want, chosen, window) notices already returned.
var guardSeen sync.Map

func guardNotice(want, got Profile, window int) string {
	if _, seen := guardSeen.LoadOrStore(fmt.Sprintf("%s>%s@%d", want, got, window), true); seen {
		return ""
	}
	size := config.FormatTokenCount(window)
	msg := fmt.Sprintf("Persona: using the %s profile instead of %s: the context window (%s tokens) is too small for it; the persona may take at most a quarter of the window. If the model's window is larger, set context_limit in your config.",
		got, want, size)
	if got == ProfileOff {
		msg = fmt.Sprintf("Persona: even the lite profile would take over half of the %s-token context window, so only the persona's identity, honesty rule and voice boundary are kept. If the model's window is larger, set context_limit in your config.",
			size)
	}
	return msg
}
