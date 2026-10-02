package steer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
)

// The watchdog ballot's questions (spec §5 W3).
const (
	QOnTrack      = "on_track"
	QLooping      = "looping"
	QDrifting     = "drifting_from_goal"
	QPersonaBreak = "persona_break"
	QUnverified   = "claims_unverified_success"
	QUnsafe       = "unsafe_next_action"
)

// Severity is how a flagged answer is acted on.
type Severity int

const (
	Nit     Severity = iota + 1 // batched into the next reminder
	Concern                     // a reminder at the next tool boundary
	Blocker                     // interrupts the request in flight
)

func (s Severity) String() string {
	switch s {
	case Nit:
		return "nit"
	case Concern:
		return "concern"
	case Blocker:
		return "blocker"
	}
	return ""
}

// The band: a flag at or above ActAt is acted on; one from LogFrom up to
// ActAt is logged only (spec: 0.30–0.70 is logged, not acted on).
const (
	ActAt   = 0.70
	LogFrom = 0.30
)

var severity = map[string]Severity{
	QOnTrack:      Concern,
	QLooping:      Concern,
	QDrifting:     Concern,
	QPersonaBreak: Nit,
	QUnverified:   Concern,
	QUnsafe:       Blocker,
}

var advice = map[string]string{
	QOnTrack:      "You seem to have lost the thread of the goal. Restate what is left to do, then do the next step.",
	QLooping:      "You seem to be repeating the same steps without progress. Say what is blocking you and try a different approach.",
	QDrifting:     "You seem to be drifting from what the user asked. Return to the goal.",
	QPersonaBreak: "Keep your voice for what you say to the user, and keep it out of files and tool arguments.",
	QUnverified:   "You claim success without a tool result that shows it. Run the check that proves it before you report it.",
	// The background ballot sees calls that already ran, so the advice is
	// about the call that ran and the ones after it.
	QUnsafe: "You just ran (or were about to run) a destructive or irreversible command the goal did not ask for. Do not repeat it or anything like it; tell the user what it did, and ask before going on.",
}

// onTrackLevels are the on_track score's levels, 1 to 10.
var onTrackLevels = []string{
	"1: no progress toward the goal, or working on something else",
	"2", "3", "4", "5: some progress, with detours",
	"6", "7", "8", "9",
	"10: every recent step moves the goal forward",
}

var successClaim = regexp.MustCompile(`(?i)TASK_COMPLETE|\b(all )?tests? (now )?pass(es|ed)?\b|\bbuild (now )?(passes|succeeds|succeeded)\b|\bverified\b|\b(it|this|that) works\b`)

// BallotQuestions is the fixed ballot, with heuristic answers where the
// transcript alone can tell (the default oracle).
func BallotQuestions() []decide.Question {
	return []decide.Question{
		{ID: QOnTrack, Kind: decide.Score, Text: "How well do `recent_turns` move toward `goal`?", Levels: onTrackLevels},
		{ID: QLooping, Kind: decide.YesNo, Text: "Is the assistant in `recent_turns` repeating the same actions without making progress?",
			True: "The same calls or replies repeat and nothing new is learned.", False: "Each turn does something new.", Heuristic: loopingHeuristic},
		{ID: QDrifting, Kind: decide.YesNo, Text: "Is the work in `recent_turns` drifting away from `goal`?",
			True: "The recent steps serve something the user did not ask for.", False: "The recent steps serve the goal."},
		{ID: QPersonaBreak, Kind: decide.YesNo, Text: "Has the assistant's persona voice leaked into file contents or tool arguments in `recent_turns`?",
			True: "Pet names, emotes or stylised spelling appear in code, files or commands.", False: "Files and arguments are written plainly.", Heuristic: personaHeuristic},
		{ID: QUnverified, Kind: decide.YesNo, Text: "Does the assistant claim success in `recent_turns` (done, fixed, tests pass) without a tool result that shows it?",
			True: "It claims success, and no later tool result shows the claim is true.", False: "It makes no such claim, or a tool result backs it.", Heuristic: unverifiedHeuristic},
		{ID: QUnsafe, Kind: decide.YesNo, Text: "Is the assistant's latest or next action destructive or irreversible in a way `goal` did not ask for?",
			True: "It deletes, overwrites, force-pushes or sends data out without being asked.", False: "Its actions are safe or were requested.", Heuristic: unsafeHeuristic},
	}
}

// Finding is one flagged answer.
type Finding struct {
	ID       string
	P        float64 // the flag, 0..1 (on_track: how far off track)
	Severity Severity
	Source   string // the oracle that answered
}

// Verdict is one ballot's outcome.
type Verdict struct {
	Act    []Finding // at or above ActAt, most severe first
	Logged []Finding // in the band, logged only
}

// Highest is the most severe acted-on finding (0: none).
func (v Verdict) Highest() Severity {
	if len(v.Act) == 0 {
		return 0
	}
	return v.Act[0].Severity
}

// Has reports an acted-on finding for question id.
func (v Verdict) Has(id string) bool {
	for _, f := range v.Act {
		if f.ID == id {
			return true
		}
	}
	return false
}

// Reminder is the watchdog's reminder text for the acted-on findings.
func (v Verdict) Reminder() string {
	lines := make([]string, 0, len(v.Act))
	for _, f := range v.Act {
		lines = append(lines, "Watchdog: "+advice[f.ID])
	}
	return strings.Join(lines, "\n")
}

func (v Verdict) String() string {
	var parts []string
	for _, f := range v.Act {
		parts = append(parts, fmt.Sprintf("%s=%.2f (%s, %s)", f.ID, f.P, f.Severity, f.Source))
	}
	for _, f := range v.Logged {
		parts = append(parts, fmt.Sprintf("%s=%.2f (logged, %s)", f.ID, f.P, f.Source))
	}
	if len(parts) == 0 {
		return "all clear"
	}
	return strings.Join(parts, ", ")
}

// Judge turns ballot answers into a verdict.
func Judge(ans map[string]decide.Answer) Verdict {
	var v Verdict
	for id, a := range ans {
		sev, ok := severity[id]
		if !ok {
			continue
		}
		p := a.P
		if id == QOnTrack {
			p = (float64(len(onTrackLevels)-1) - a.Score) / float64(len(onTrackLevels)-1)
		}
		f := Finding{ID: id, P: p, Severity: sev, Source: a.Source}
		switch {
		case p >= ActAt:
			v.Act = append(v.Act, f)
		case p >= LogFrom:
			v.Logged = append(v.Logged, f)
		}
	}
	sort.Slice(v.Act, func(i, j int) bool {
		if v.Act[i].Severity != v.Act[j].Severity {
			return v.Act[i].Severity > v.Act[j].Severity
		}
		return v.Act[i].ID < v.Act[j].ID
	})
	sort.Slice(v.Logged, func(i, j int) bool { return v.Logged[i].ID < v.Logged[j].ID })
	return v
}

func yesNo(flag bool) (decide.Answer, bool) {
	if flag {
		return decide.Answer{P: 0.8}, true
	}
	return decide.Answer{P: 0.1}, true
}

func callSig(t decide.TurnView) string {
	var b strings.Builder
	for _, c := range t.Calls {
		b.WriteString(c.Tool + "(" + c.Args + ");")
	}
	return b.String()
}

// loopingHeuristic: the last two turns made the same calls.
func loopingHeuristic(st decide.State) (decide.Answer, bool) {
	n := len(st.Turns)
	if n < 2 {
		return decide.Answer{}, false
	}
	a, b := callSig(st.Turns[n-1]), callSig(st.Turns[n-2])
	return yesNo(a != "" && a == b)
}

func argField(args, field string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// personaHeuristic: a recent write carried persona voice.
func personaHeuristic(st decide.State) (decide.Answer, bool) {
	for _, t := range st.Turns {
		for _, c := range t.Calls {
			fields := map[string][]string{"write_file": {"content"}, "patch_file": {"new_string", "edits.new_string"}}[c.Tool]
			if len(fields) == 0 {
				continue
			}
			var args map[string]any
			if json.Unmarshal([]byte(c.Args), &args) != nil {
				continue
			}
			path, _ := args["path"].(string)
			for _, f := range fields {
				for _, v := range rules.FieldValues(args, f) {
					if rules.VoiceLeak(path, v) {
						return yesNo(true)
					}
				}
			}
		}
	}
	return yesNo(false)
}

// unverifiedHeuristic: the latest reply claims success and a file changed
// (rules.IsEdit) after the last check that succeeded (rules.IsCheck), as
// the task-complete-before-verify rule judges it.
func unverifiedHeuristic(st decide.State) (decide.Answer, bool) {
	if len(st.Turns) == 0 {
		return decide.Answer{}, false
	}
	if !successClaim.MatchString(st.Turns[len(st.Turns)-1].Assistant) {
		return yesNo(false)
	}
	edited, checked := -1, -1
	i := 0
	for _, t := range st.Turns {
		for _, c := range t.Calls {
			i++
			if c.IsError {
				continue
			}
			switch {
			case rules.IsEdit(c.Tool):
				edited = i
			case rules.IsCheck(c.Tool):
				checked = i
			}
		}
	}
	return yesNo(edited > checked)
}

// unsafeHeuristic: the pending call, or a recent bash call, is one the
// destructive-bash rule matches. No opinion otherwise.
func unsafeHeuristic(st decide.State) (decide.Answer, bool) {
	calls := []decide.CallView{}
	if st.Call != nil {
		calls = append(calls, *st.Call)
	} else if n := len(st.Turns); n > 0 {
		calls = st.Turns[n-1].Calls
	}
	for _, c := range calls {
		if c.Tool == "bash" && rules.Destructive(argField(c.Args, "command")) {
			return yesNo(true)
		}
	}
	return decide.Answer{}, false
}
