package prompts

import (
	"strings"

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

// voiceBoundaryPrompt keeps the persona out of artifacts. It follows the
// persona core directly so it frames everything after it, including the
// slider block.
const voiceBoundaryPrompt = `Voice Boundary:
Your voice, personality and the voice modulation below apply only to prose you address to the user. Code, code comments, commit messages, file contents, and tool-call arguments are written plainly and professionally: no persona voice, emotes, pet names, or stylised spelling. Where a tool's instructions and a voice instruction conflict, the tool's instructions win.`

// PersonaLevel picks how much of Celeste's persona a prompt carries. The
// zero value is the full persona; PersonaOff is the only other level. No
// level composes an empty persona section.
type PersonaLevel string

// PersonaOff is Celeste's "off" level, for the internal review and research
// lanes (a typed explore or review subagent, 2.0 W4e): her identity line,
// the honesty rule and the voice boundary, with no persona core, user
// identity, sliders or chat rules. W5-A's rebase maps it to its ProfileOff.
const PersonaOff PersonaLevel = "off"

// offIdentity and offHonesty are W5-A's public persona text
// (publicIdentity, publicHonesty in its profile.go); offPersona joins them
// with the voice boundary the way its public fallback does, so W5-A can
// swap offPersona for its ProfileOff in one line.
const (
	offIdentity = "You are Celeste, the AI companion in the celeste command-line tool, created by whyKusanagi."
	offHonesty  = "Never say that a file was written, audio was saved or any other action happened unless a tool actually returned that result this turn."
	offPersona  = offIdentity + "\n\n" + offHonesty + "\n\n" + voiceBoundaryPrompt
)

// ComposeOptions describes one system prompt.
type ComposeOptions struct {
	Mode Mode
	// PersonaLevel is the persona level; empty (or anything but PersonaOff)
	// is the full persona. No config sets it.
	PersonaLevel PersonaLevel
	// Contract is the agent operating contract. Used in ModeAgent only.
	Contract string
	// Sliders overrides slider.json for this prompt (a subagent's persona
	// override). Nil loads slider.json.
	Sliders *config.SliderConfig
	// ProjectContext is the grimoire, project memories and code-graph summary.
	ProjectContext string
	// GitSnapshot is the formatted git state.
	GitSnapshot string
}

// confirmActionsEnabled reports whether the user has turned on confirm mode.
// A variable so tests don't depend on the real config file.
var confirmActionsEnabled = func() bool {
	cfg, err := config.Load()
	return err == nil && cfg.ConfirmActions
}

// Compose builds a system prompt. Every caller goes through here so a prompt
// refresh or endpoint switch produces the same prompt as session start.
//
// Order: persona core (byte-stable, so the prefix stays cacheable), voice
// boundary, user identity, sliders, mode contract, project context, git.
func Compose(opts ComposeOptions) string {
	sections := []string{offPersona}
	if opts.PersonaLevel != PersonaOff {
		sections[0] = personaSection(opts)
	}
	if opts.Mode == ModeAgent && opts.Contract != "" {
		sections = append(sections, opts.Contract)
	}
	if opts.ProjectContext != "" {
		sections = append(sections, "# Project Context (.grimoire)\n\n"+opts.ProjectContext)
	}
	if opts.GitSnapshot != "" {
		sections = append(sections, opts.GitSnapshot)
	}
	return strings.Join(sections, "\n\n")
}

// personaSection is the persona core, voice boundary, user identity,
// sliders and, in chat, the chat rules.
func personaSection(opts ComposeOptions) string {
	persona := []string{personaCore(), voiceBoundaryPrompt}
	if user := ComposeUserPrompt(config.LoadUser()); user != "" {
		persona = append(persona, user)
	}
	sliders := opts.Sliders
	if sliders == nil {
		sliders = config.LoadSliders()
	}
	if block := ComposeSliderPrompt(sliders); block != "" {
		persona = append(persona, block)
	}
	if opts.Mode == ModeChat {
		persona = append(persona, taskExecutionPrompt)
		if confirmActionsEnabled() {
			persona = append(persona, confirmModePrompt)
		}
	}

	return strings.TrimRight(strings.Join(persona, "\n"), "\n")
}

// personaCore returns the persona text from the essence.
func personaCore() string {
	essence, err := LoadEssence()
	if err != nil {
		// Only reachable if the embedded essence itself is broken, which
		// TestEmbeddedEssenceIsValid guards against. User overrides that
		// fail to load fall back to the embedded essence inside LoadEssence.
		return getBasicPrompt()
	}
	return buildPromptFromEssence(essence)
}
