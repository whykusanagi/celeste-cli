package prompts

import (
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Mode selects the operating contract a system prompt carries.
type Mode int

const (
	// ModeChat is the interactive conversation (TUI chat, one-shot messages,
	// the MCP chat handler): the chat task-execution rules, plus confirm mode
	// when the user has turned it on.
	ModeChat Mode = iota
	// ModeAgent is an agent or subagent run. It carries the agent contract
	// supplied by the runtime and never the chat rules or confirm mode: agent
	// runs are often headless, and the permission checker is what gates their
	// actions (#170).
	ModeAgent
)

// PersonaLevel picks how much of Celeste's persona a prompt carries. The
// zero value is the full persona; PersonaOff is the only other level. No
// level composes an empty persona section.
type PersonaLevel string

// PersonaOff is Celeste's "off" level, for the internal lanes that report to
// the CLI rather than talk to the user: typed explore and review subagents
// (2.0 W4e), orchestrator lanes and the debate reviewer. It is her identity
// line and the honesty rule (publicPreamble), then the ProfileOff profile
// (the voice boundary rule, sealed or public), with no full profile, user
// identity, sliders or chat rules. Never less than that (owner ruling on
// #265). The CLI adds identity and honesty itself, for sealed and public
// builds alike, because the sealed off profile is the voice boundary only.
const PersonaOff PersonaLevel = "off"

// ProfileFor is the mode's persona profile (spec W5 "Profiles by mode"):
// full for chat, spine for agent runs.
func ProfileFor(m Mode) Profile {
	if m == ModeAgent {
		return ProfileSpine
	}
	return ProfileFull
}

// Prompt is one system prompt in two parts (spec W5 "Byte-stable assembly").
// Static is the persona: a profile's bytes, unchanged by anything
// per-request, so provider prefix caches hit. Dynamic is everything else, in
// the spec's order.
type Prompt struct {
	Static  string
	Dynamic string
	// Profile is the profile in Static.
	Profile Profile
	// Notice is the small-window guard's one-time message, for the caller
	// to show; "" otherwise.
	Notice string
}

// String is the whole prompt: Static, a blank line, Dynamic.
func (p Prompt) String() string {
	switch {
	case p.Static == "":
		return p.Dynamic
	case p.Dynamic == "":
		return p.Static
	}
	return p.Static + "\n\n" + p.Dynamic
}

// ComposeOptions describes one system prompt.
type ComposeOptions struct {
	Mode Mode
	// PersonaLevel is the persona level; empty (or anything but PersonaOff)
	// is the mode's profile (ProfileFor). No config sets it.
	PersonaLevel PersonaLevel
	// Window is the model's resolved context window in tokens
	// (config.ResolveContextLimit); 0 means unknown: no small-window guard.
	Window int
	// Contract is the agent operating contract. Used in ModeAgent only.
	Contract string
	// Sliders overrides slider.json for this prompt (a subagent's persona
	// override). Nil loads slider.json.
	Sliders *config.SliderConfig
	// ProjectContext is the grimoire, context files, code-graph summary and
	// any SessionStart context.
	ProjectContext string
	// GitSnapshot is the formatted git state.
	GitSnapshot string
	// Memories is the "# Project Memories" block; it follows git.
	Memories string
}

// confirmActionsEnabled reports whether the user has turned on confirm mode.
// A variable so tests don't depend on the real config file.
var confirmActionsEnabled = func() bool {
	cfg, err := config.Load()
	return err == nil && cfg.ConfirmActions
}

// now is the clock behind the date line; tests pin it.
var now = time.Now

// Compose builds a system prompt. Every caller goes through here so a prompt
// refresh or endpoint switch produces the same prompt as session start.
//
// Static: the persona profile (personaStatic), stepped down by the
// small-window guard (selectProfile) when opts.Window is known. Dynamic, in order (W5 ruling
// 8): sliders, user identity, mode rules (the chat rules and confirm mode,
// or the agent contract), project context, git, memories, the date (day
// granularity, so a day's prompts share bytes). The off profile has no
// voice to modulate, so it gets no sliders or identity, and the PersonaOff
// level (a reporting lane) gets no chat rules either; a chat the guard put
// on off keeps them, confirm mode included.
func Compose(opts ComposeOptions) Prompt {
	want := ProfileFor(opts.Mode)
	if opts.PersonaLevel == PersonaOff {
		want = ProfileOff
	}
	pp, notice := selectProfile(want, opts.Window)
	p := Prompt{Static: personaStatic(pp), Profile: pp.Profile, Notice: notice}
	var dynamic []string
	if pp.Profile != ProfileOff {
		sliders := opts.Sliders
		if sliders == nil {
			sliders = config.LoadSliders()
		}
		dynamic = append(dynamic, ComposeSliderPrompt(sliders), ComposeUserPrompt(config.LoadUser()))
	}
	switch {
	case opts.Mode == ModeAgent:
		dynamic = append(dynamic, opts.Contract)
	case opts.PersonaLevel != PersonaOff:
		dynamic = append(dynamic, taskExecutionPrompt)
		if confirmActionsEnabled() {
			dynamic = append(dynamic, confirmModePrompt)
		}
	}
	if opts.ProjectContext != "" {
		dynamic = append(dynamic, "# Project Context (.grimoire)\n\n"+opts.ProjectContext)
	}
	dynamic = append(dynamic, opts.GitSnapshot, opts.Memories, "Current date: "+now().Format("2006-01-02"))
	p.Dynamic = joinSections(dynamic)
	return p
}

// personaStatic is a profile's Static text. The off profile is the voice
// boundary rule alone, so Celeste's identity line and the honesty rule
// (publicPreamble) go before it: no prompt carries less (owner ruling on
// #265).
func personaStatic(pp *PersonaProfile) string {
	if pp.Profile == ProfileOff {
		return publicPreamble + "\n\n" + pp.SystemPrompt
	}
	return pp.SystemPrompt
}

// joinSections joins the non-empty sections with a blank line.
func joinSections(parts []string) string {
	var out []string
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}
