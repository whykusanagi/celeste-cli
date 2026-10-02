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

// ComposeOptions describes one system prompt.
type ComposeOptions struct {
	Mode Mode
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
// Order: persona profile (byte-stable, ending with the voice boundary rule),
// user identity, sliders, mode contract, project context, git.
func Compose(opts ComposeOptions) string {
	persona := []string{personaCore()}
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

	sections := []string{strings.TrimRight(strings.Join(persona, "\n"), "\n")}
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

// personaCore returns the full profile, official or public. Either ends
// with the voice boundary rule.
func personaCore() string {
	return mustProfile(ProfileFull).SystemPrompt
}
