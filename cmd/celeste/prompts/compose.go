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

// ComposeOptions describes one system prompt.
type ComposeOptions struct {
	Mode Mode
	// SkipPersona leaves out the persona core, voice boundary, user identity
	// and sliders (skip_persona_prompt).
	SkipPersona bool
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
	var persona []string
	if !opts.SkipPersona {
		persona = append(persona, personaCore(), voiceBoundaryPrompt)
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
	}

	switch opts.Mode {
	case ModeChat:
		// The chat rules are part of the persona prompt; with the persona
		// skipped, chat has always run with no system rules at all.
		if !opts.SkipPersona {
			persona = append(persona, taskExecutionPrompt)
			if confirmActionsEnabled() {
				persona = append(persona, confirmModePrompt)
			}
		}
	}

	var sections []string
	if len(persona) > 0 {
		sections = append(sections, strings.TrimRight(strings.Join(persona, "\n"), "\n"))
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
