// Celeste CLI - Interactive AI Assistant with Bubble Tea TUI
// This file provides the new main entry point using Bubble Tea.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/commands"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/monitor"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/server"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/steer"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/subagents"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Version information — injected at build time via ldflags.
// CI/CD sets these: go build -ldflags "-X main.Version=1.8.0 -X main.Build=bubbletea-tui -X main.CommitSHA=abc123"
// When not set by ldflags, defaults are used.
var (
	Version   = "1.16.0" // x-release-please-version
	Build     = "bubbletea-tui"
	CommitSHA = "dev"
	// Channel is "release" only in official release binaries (release.yml
	// sets -X main.Channel=release). celeste update uses it to tell an
	// official binary from a local build (W5 ruling 29).
	Channel = "source"
)

// Global config name (set by -config flag)
var configName string
var maxToolIterationsOverride int

// hasDefaultConfig checks if a default configuration file exists.
func hasDefaultConfig() bool {
	configPath := config.NamedConfigPath("") // Empty name = default config
	_, err := os.Stat(configPath)
	return err == nil
}

// printUsage prints the CLI usage information.
func printUsage() {
	fmt.Print(`
✨ Celeste CLI - Interactive AI Assistant

Usage:
  celeste [-config <name>] <command> [arguments]

Global Flags:
  -config <name>          Use named config (loads ~/.celeste/config.<name>.json)
  -max-tool-iterations N  Override the tool-loop turn cap for chat's starting
                           profile only; not carried across /endpoint or a
                           resumed session, and ignored by agent/message

Commands:
  chat                    Launch interactive TUI mode
  message <text>          Send a single message and exit
  config                  View/modify configuration
  skills                  List and manage skills
  providers               List and query AI providers
  agent                   Run autonomous agent loops for complex tasks
  session                 Manage conversation sessions
  context                 Show context/token usage
  stats                   Show usage statistics
  export                  Export session data
  init [--agents]         Create a starter .grimoire (and AGENTS.md) for the current project
  grimoire                Show the resolved project grimoire (all layers merged)
  index [status|rebuild|reset]  Manage code graph index
  serve                   Start MCP server (stdio or SSE transport)
  wallet-monitor          Manage wallet security monitoring daemon
  costs                   Show session cost breakdown
  memories                List memories for current project
  remember "<text>"       Save a memory
  forget <name>           Delete a memory
  resume [session-id]     Resume a previous session
  plan [show]             Show current plan from .celeste/plan.md
  revert <file> [--session id] [--force]  Restore a file from its last checkpoint
  hooks [list|trust]      Inspect lifecycle hooks and approve repo hooks
  update [--check]        Install the latest official release (go install and release builds)
  help                    Show this help message
  version                 Show version information

Interactive Commands (in chat mode):
  /help                   Show available commands
  /clear                  Clear chat history
  /config                 Show current configuration
  /tools, /skills         Browse available tools
  /agent <goal>           Run autonomous task loop
  /orch <goal>            Multi-model orchestrated run
  /memories               List project memories
  /costs                  Show session costs
  /context                Show context/token usage
  /diff                   List the files this session changed
  /undo                   Undo the last file change (repeat to go back)
  /grimoire               Show project grimoire
  /index                  Show code graph status
  /plan [show]            Show current plan
  /effort <level>         Set reasoning effort (off/low/medium/high/max)
  /endpoint <name>        Switch AI provider endpoint
  /model <name>           Change the model
  exit, quit, q           Exit the application

Keyboard Shortcuts:
  Ctrl+C                  Cancel current operation (double-tap to exit)
  PgUp/PgDown            Scroll chat history
  Shift+↑/↓              Scroll chat history
  ↑/↓                    Navigate input history

Configuration:
  celeste config --show                  Show current config
  celeste config --list                  List all config profiles
  celeste config --init <name>           Create a new config profile
  celeste config --init jev              Save a TypeSafe key; Jev in shadow mode
  celeste config --set-key <key>         Set API key
  celeste config --set-url <url>         Set API URL
  celeste config --set-model <model>     Set model
  celeste config --set-max-tool-iterations <n>
                                          Set the chat's tool-loop turn cap
  celeste config --set-context-limit <tokens>
                                          Set the context window (0 = model default)

Skills:
  celeste skills --list                  List available skills
  celeste skills --init                  Create default skill files
  celeste skills --delete <name>         Delete a skill
  celeste skills --info <name>           Show skill information
  celeste skills --reload                Reload skills from disk
  celeste skill <name> [--args]          Execute a skill

Providers:
  celeste providers                      List all AI providers
  celeste providers --tools              List tool-capable providers
  celeste providers info <name>          Show provider details
  celeste providers current              Show current provider

Sessions:
  celeste session --list                 List saved sessions
  celeste session --load <id>            Load a session
  celeste session --clear                Clear all sessions

Agent:
  celeste agent --goal "<task>"          Run autonomous task loop
  celeste agent --resume <run-id>        Resume checkpointed run
  celeste agent --list-runs              List recent runs
  celeste agent --eval <cases.json>      Run eval harness cases
  celeste agent --benchmark <suite.json> Run benchmark suite scaffolding
  celeste agent --planner=true --verify-cmd "go test ./..." --require-verify
                                          Enable plan->execute->verify gating

Environment Variables:
  CELESTE_API_KEY         API key (overrides the config file)
  CELESTE_API_ENDPOINT    API endpoint (overrides the config file)
  VENICE_API_KEY          Venice.ai API key for NSFW mode (fallback when skills.json has none)
  TAROT_AUTH_TOKEN        Tarot function auth token (overrides the config file)

Examples:
  celeste chat                           Start with default config
  celeste -config openai chat            Start with OpenAI config
  celeste -config grok chat              Start with Grok/xAI config
  celeste agent --goal "refactor this package and add tests"
  celeste config --list                  List available configs
  celeste config --init openai           Create OpenAI config template
`)
}

// runChatTUI launches the interactive Bubble Tea TUI.
func runChatTUI() {
	// Migrate every profile once, before the alt screen opens: a later
	// /endpoint or SwitchEndpoint load of a different profile, while the TUI
	// is already running, then finds nothing left to migrate instead of
	// printing notes into the alt screen on every switch (#144 W6b review,
	// I1(a)).
	config.MigrateConfigDir()

	// Load configuration (named or default)
	cfg, err := config.LoadNamedWithEnv(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if maxToolIterationsOverride > 0 {
		cfg.MaxToolIterations = maxToolIterationsOverride
	}
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = config.DefaultMaxToolIterations
	}

	// Show which config is being used
	if configName != "" {
		fmt.Fprintf(os.Stderr, "Using config: %s\n", configName)
	}

	// Validate API key — not required for Google ADC/service-account auth or
	// a keyless local endpoint (#151).
	if cfg.APIKey == "" && needsAPIKey(cfg) {
		fmt.Fprintln(os.Stderr, "No API key configured.")
		if configName != "" {
			fmt.Fprintf(os.Stderr, "Edit %s or set CELESTE_API_KEY\n", config.NamedConfigPath(configName))
		} else {
			fmt.Fprintln(os.Stderr, "Set CELESTE_API_KEY environment variable or run: celeste config --set-key <key>")
		}
		os.Exit(1)
	}

	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	// Repo hooks are approved on the terminal before the TUI takes it over:
	// with chatHookApprover nil, loop.Setup prompts only when stdin and
	// stderr are both terminals (piped consoles never approve; use
	// `celeste hooks trust`).
	app, deps, err := newChatApp(cfg, cwd, homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	registry, tuiClient := deps.registry, deps.adapter
	defer deps.env.Close() // last: MCP clients and the code graph
	defer tuiClient.subMgr.Close()
	defer tui.CloseLogging()
	defer tuiClient.shutdown(3 * time.Second) // cancel running turns and wait for them

	// Run the TUI
	// Mouse capture disabled — allows terminal-native text selection and copy.
	p := tea.NewProgram(app, tea.WithAltScreen())
	notify := func(s string) { p.Send(tui.HookWarningMsg{Text: s}) }
	hookNotify.Store(&notify)
	defer hookNotify.Store(nil)

	// The permission and ask modals (chat_prompts.go): they run off the
	// Update loop and reach it through p.Send.
	promptFn := permissionPrompt(p.Send)
	registry.SetPromptFunc(promptFn)
	tuiClient.promptFn = promptFn
	registry.SetAskFunc(askPrompt(p.Send))

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	// Print log path on exit
	if logPath := tui.GetLogPath(); logPath != "" {
		fmt.Printf("\nSkill call log: %s\n", logPath)
	}
}

// TUIClientAdapter adapts the LLM client for the TUI.
type TUIClientAdapter struct {
	client      *llm.Client
	registry    *tools.Registry
	baseConfig  *config.Config // Store base config for loading named configs
	costTracker *costs.SessionTracker
	subMgr      *subagents.Manager // exposed for /agents TUI command

	// promptFn shows the TUI permission modal; /agent runs use it to approve
	// tools (#172). Set once the Bubble Tea program exists.
	promptFn tools.PromptFunc
	// parentEnv is the chat's Env, which /agent runners nest under (2.0
	// F2e); nil (tests that build an adapter by hand) gives each runner its
	// own Setup.
	parentEnv loop.Nester

	// snapshots is the chat session's checkpoint store, for /undo and
	// /diff (2.0 F4); nil turns both off. workspace relativizes their
	// paths. undoConfirm is what a first /undo refused to undo because
	// the file changed afterwards; /undo again undoes it while the file is
	// still as warned about.
	snapshots   *checkpoints.SnapshotManager
	workspace   string
	undoConfirm *undoWarning

	// pruned holds tool results that context compaction removed (#174);
	// created on first use.
	pruned *compact.Store
	// summarize writes compaction summaries with the small-model role;
	// created on first use.
	summarize compact.SummarizeFunc
	// jev judges pruning in shadow mode when jev_prune is "shadow" (#175);
	// resolved on first use.
	jev    *jev.Client
	jevFor *config.Config // the config jev was resolved for
	// rules are the chat Env's stream rules; steer is the chat session's
	// steering (2.0 W3), rebuilt when a profile switch replaces baseConfig.
	rules    *rules.Set
	steer    *steer.Session
	steerFor *config.Config
	steerSet bool

	// Session-start project context (grimoire, memories, code graph) and git
	// snapshot, kept so a prompt refresh or endpoint switch doesn't drop them.
	projectContext string
	gitSnapshot    string
	// hooks runs the session's lifecycle hooks (2.0 F0); nil allows all.
	hooks *hooks.Runner
	// lifeCtx lives as long as the chat app; Stop hooks use it, so they
	// are cancelled on exit rather than outliving the program.
	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	// gate answers the chat loop's permission asks with the modal
	// (chatGate); nil denies (tests that build an adapter by hand).
	gate loop.Gate
	// compactMu serializes compactWith: the loop's compactor calls it on
	// a run goroutine, /context compact on the Update goroutine.
	compactMu sync.Mutex
	// Running turns, so shutdown can wait for them before the Env closes.
	runsMu  sync.Mutex
	running int
	closing bool
	idle    chan struct{}
	// spillSeq numbers spill files across the session's turns
	// (loop.Loop.SpillCounter), so a call ID repeated in a later turn
	// never overwrites an earlier spill.
	spillSeq atomic.Int64
}

// systemPrompt composes the chat system prompt for the current config,
// including the session's project context.
func (a *TUIClientAdapter) systemPrompt() string {
	return prompts.GetSystemPromptWithContext(a.projectContext, a.gitSnapshot)
}

// GetSkills implements tui.LLMClient.
func (a *TUIClientAdapter) GetSkills() []tui.SkillDefinition {
	return a.client.GetSkills()
}

// SwitchEndpoint switches to a different endpoint by loading its named config.
func (a *TUIClientAdapter) SwitchEndpoint(endpoint string) error {
	prevProvider := ""  // set when falling back to the base config
	targetKey := false  // the fallback found a key for the target provider
	fallbackModel := "" // skills.json's Venice model, for that fallback
	// Try to load named config for the endpoint
	cfg, err := config.LoadNamed(endpoint)
	if err != nil {
		// If named config doesn't exist, use a copy of the base config with
		// a modified base URL (never the base config itself: the chat's
		// startup config must not change under it).
		base := *a.baseConfig
		cfg = &base
		prevProvider = providers.DetectProvider(cfg.BaseURL)

		// For Venice, try to load from skills.json first
		if endpoint == "venice" {
			skillsConfig, err := config.LoadSkillsConfig()
			if err == nil && skillsConfig.VeniceAPIKey != "" {
				cfg.APIKey = skillsConfig.VeniceAPIKey
				targetKey = true
				cfg.BaseURL = skillsConfig.VeniceBaseURL
				fallbackModel = skillsConfig.VeniceModel
				tui.LogInfo("Loaded Venice configuration from skills.json")
			} else {
				// Fall back to environment variables
				if veniceKey := os.Getenv("VENICE_API_KEY"); veniceKey != "" {
					cfg.APIKey = veniceKey
					targetKey = true
					tui.LogInfo("Using VENICE_API_KEY from environment")
				} else {
					tui.LogInfo("Warning: No VENICE_API_KEY found, using default API key (will likely fail)")
				}

				// Check for custom base URL
				if envURL := os.Getenv("VENICE_API_BASE_URL"); envURL != "" {
					cfg.BaseURL = envURL
				} else {
					cfg.BaseURL = "https://api.venice.ai/api/v1"
				}
			}
		} else {
			// Map endpoint names to base URLs
			endpointURLs := map[string]string{
				"openai":     "https://api.openai.com/v1",
				"grok":       "https://api.x.ai/v1",
				"elevenlabs": "https://api.elevenlabs.io/v1",
				"google":     "https://generativelanguage.googleapis.com/v1",
			}

			if url, ok := endpointURLs[endpoint]; ok {
				cfg.BaseURL = url
				tui.LogInfo(fmt.Sprintf("Using fallback URL for %s: %s", endpoint, url))
			} else {
				tui.LogInfo(fmt.Sprintf("Warning: Unknown endpoint '%s', keeping current URL", endpoint))
			}
		}
	} else {
		tui.LogInfo(fmt.Sprintf("Loaded named config for endpoint: %s", endpoint))
	}
	if prevProvider != "" && providers.DetectProvider(cfg.BaseURL) != prevProvider && !targetKey {
		// The startup key (CELESTE_API_KEY included) belongs to the old
		// provider: never send it to another. The request then fails with
		// the normal "no API key" message.
		cfg.APIKey = ""
	}
	if prevProvider != "" && providers.DetectProvider(cfg.BaseURL) != prevProvider {
		// The previous provider's models mean nothing here: drop them so
		// the new provider's default is adopted, without a false "no
		// longer serves" note.
		cfg.Model, cfg.AgentModel, cfg.SmallModel = "", "", ""
	}
	if fallbackModel != "" {
		cfg.Model = fallbackModel
	}
	// The agent and small models resolve from what this process already
	// knows (no I/O: this runs in the TUI's Update); the chat model is the
	// TUI's to resolve, so it can say so in the chat.
	cfg = servedAgentModels(cfg)

	// Update LLM client configuration
	llmConfig := llm.ConfigFrom(cfg)

	a.client.UpdateConfig(llmConfig)

	// Persist the full config as baseConfig so that agent/orchestrator commands
	// pick up provider-specific settings like Orchestrator lanes.
	a.baseConfig = cfg
	a.summarize = nil // built for the old endpoint's small model

	// Recompose the prompt for the new config, keeping the project context.
	a.client.SetSystemPrompt(a.systemPrompt())
	tui.LogInfo("✓ Celeste persona prompt re-injected after endpoint switch")

	// Log the switch with masked API key
	maskedKey := "none"
	if len(cfg.APIKey) > 8 {
		maskedKey = cfg.APIKey[:4] + "..." + cfg.APIKey[len(cfg.APIKey)-4:]
	} else if cfg.APIKey != "" {
		maskedKey = "***"
	}
	tui.LogInfo(fmt.Sprintf("✓ Switched endpoint to: %s", endpoint))
	tui.LogInfo(fmt.Sprintf("  URL: %s", providers.CleanBaseURL(cfg.BaseURL)))
	tui.LogInfo(fmt.Sprintf("  Model: %s", cfg.Model))
	tui.LogInfo(fmt.Sprintf("  API Key: %s", maskedKey))
	return nil
}

// ActiveEndpoint implements tui.ActiveEndpointer: the endpoint the client is
// on now, so the chat can resolve its model against what it serves.
func (a *TUIClientAdapter) ActiveEndpoint() tui.ActiveEndpoint {
	c := a.client.GetConfig()
	ep := tui.ActiveEndpoint{
		Provider: providers.DetectProvider(c.BaseURL),
		BaseURL:  c.BaseURL,
		APIKey:   c.APIKey,
		Model:    c.Model,
	}
	b := a.baseConfig
	if b == nil {
		b = &config.Config{} // CELESTE_PIN_MODEL still counts
	}
	ep.AgentModel, ep.SmallModel = b.AgentModel, b.SmallModel
	ep.Pinned = b.ModelPinned()
	return ep
}

// RefreshServedModels implements tui.ServedModelsRefresher: after a catalog
// loads, the agent and small models are re-resolved from memory on a copy
// of the base config.
func (a *TUIClientAdapter) RefreshServedModels() {
	if a.baseConfig == nil {
		return
	}
	a.baseConfig = servedAgentModels(a.baseConfig)
	a.summarize = nil
}

// servedAgentModels returns a copy of cfg with its agent and small models
// resolved from what this process already knows (no I/O). The chat model is
// left alone: the TUI resolves it and says so in the chat.
func servedAgentModels(cfg *config.Config) *config.Config {
	c := *cfg
	probe := c
	probe.Model = ""
	notes, _ := probe.ResolveServedModelsCached()
	for _, n := range notes {
		tui.LogInfo(n)
	}
	c.AgentModel, c.SmallModel = probe.AgentModel, probe.SmallModel
	return &c
}

// ChangeModel changes the model for the current endpoint.
func (a *TUIClientAdapter) ChangeModel(model string) error {
	currentConfig := a.client.GetConfig()
	// A copy of the live config with the model replaced: nothing else the
	// client carries (credentials, typing, collections) can be lost.
	newConfig := new(llm.Config)
	*newConfig = *currentConfig
	newConfig.Model = model

	a.client.UpdateConfig(newConfig)
	tui.LogInfo(fmt.Sprintf("Changed model to: %s", model))
	return nil
}

// SetThinkingLevel implements tui.ThinkingConfigSetter.
func (a *TUIClientAdapter) SetThinkingLevel(level string) {
	enabled := level != "off"
	a.client.SetThinkingConfig(llm.ThinkingConfig{
		Enabled: enabled,
		Level:   level,
	})
	tui.LogInfo(fmt.Sprintf("Thinking config set: level=%s, enabled=%v", level, enabled))
}

// ListSubagents implements tui.SubagentLister.
func (a *TUIClientAdapter) ListSubagents() []tui.SubagentInfo {
	if a.subMgr == nil {
		return nil
	}
	runs := a.subMgr.ListRuns()
	infos := make([]tui.SubagentInfo, len(runs))
	for i, r := range runs {
		elapsed := r.EndedAt.Sub(r.StartedAt)
		if r.Status == "running" || r.Status == "waiting" {
			elapsed = time.Since(r.StartedAt)
		}
		infos[i] = tui.SubagentInfo{
			ID:      r.ID,
			TaskID:  r.TaskID,
			Name:    r.Name,
			Element: r.Element,
			Status:  r.Status,
			Turns:   r.Turns,
			Elapsed: elapsed,
			Type:    string(r.Type),
			Summary: r.Summary,
		}
	}
	return infos
}

// KillSubagent implements tui.SubagentKiller. It cancels a specific in-flight
// subagent by id or task id (task 6ffb5a7c). Returns false if no cancellable run
// matches (already finished or unknown id).
func (a *TUIClientAdapter) KillSubagent(id string) bool {
	if a.subMgr == nil {
		return false
	}
	return a.subMgr.Kill(id)
}

// ResumeSubagent implements tui.SubagentResumer.
// It continues a previously-failed subagent from its last saved checkpoint.
func (a *TUIClientAdapter) ResumeSubagent(ctx context.Context, checkpointID string) (string, error) {
	if a.subMgr == nil {
		return "", fmt.Errorf("subagent manager not available")
	}
	run, err := a.subMgr.Resume(ctx, checkpointID, nil)
	if err != nil {
		return "", err
	}
	return run.Result, nil
}

// CompactContext implements tui.ContextCompactor: it prunes old tool results
// when the history is over the compaction threshold (or always, with force)
// and returns the replacement for each pruned result (#174). /context compact
// calls it on the Update goroutine, never while a turn runs (commands wait
// for the turn).
//
// With jev_prune "on" it still only asks Jev in shadow: it runs on the
// Update goroutine, which must never wait on a network call (2.0 W3).
func (a *TUIClientAdapter) CompactContext(msgs []tui.ChatMessage, window, used int, force bool) tui.CompactOutcome {
	return a.compactWith(context.Background(), msgs, window, used, force, a.jevShadow(), config.ModeShadow)
}

// compactWith prunes with jc as the Jev scorer (nil: none) in jevMode
// ("on" or "shadow", compact.WithJev). A chat turn's compactor passes the
// client and mode RunTurn resolved, so the run goroutine never reads the
// adapter's config, which endpoint and profile switches replace on the
// Update goroutine.
func (a *TUIClientAdapter) compactWith(ctx context.Context, msgs []tui.ChatMessage, window, used int, force bool, jc *jev.Client, jevMode string) tui.CompactOutcome {
	a.compactMu.Lock()
	defer a.compactMu.Unlock()
	if est := compact.Estimate(msgs); est > used {
		used = est
	}
	overhead := used - compact.Estimate(msgs) // system prompt and tool schemas
	if a.pruned == nil {
		if store, err := compact.DefaultStore(); err == nil {
			a.pruned = store
		}
	}
	opts, report := compact.WithJev(ctx, jc, jevMode, msgs, compact.Options{Window: window, Used: used, Force: force}, tui.LogInfo, true)
	after, res := compact.Prune(msgs, opts, a.pruned)
	report(res)
	out := tui.CompactOutcome{
		StillOver: compact.Estimate(after)+overhead > compact.Threshold(window),
	}
	if !res.Pruned() {
		return out
	}
	out.Edits = make(map[string]string, len(res.Edits))
	for _, e := range res.Edits {
		out.Edits[e.ToolCallID] = e.Content
	}
	out.Summary = res.Summary()
	out.SavedTokens = res.SavedTokens
	return out
}

// jevShadow returns the Jev client when jev_prune is "shadow" or "on",
// resolved once
// per config. Reports go to the log file: the TUI owns the terminal. Update
// goroutine only (CompactContext, RunTurn).
func (a *TUIClientAdapter) jevShadow() *jev.Client {
	// Re-resolve after a profile switch replaces baseConfig, so turning
	// jev_prune off (or on) takes effect.
	if a.jevFor == a.baseConfig {
		return a.jev
	}
	a.jevFor, a.jev = a.baseConfig, nil
	if a.baseConfig == nil || a.baseConfig.JevPruneMode() == config.ModeOff {
		return nil
	}
	c, err := jev.NewFromEnv()
	if err != nil {
		tui.LogInfo("jev prune disabled: " + err.Error())
		return nil
	}
	c.Workspace, _ = os.Getwd() // paths in excerpts are sent relative to it
	tui.LogInfo("jev prune " + a.baseConfig.JevPruneMode() + ": redacted excerpts of old tool results are sent to TypeSafe")
	a.jev = c
	return c
}

// steering returns the chat session's stream rules and watchdog (2.0 W3),
// one session across turns so a rule's repeat policy and the ballot's
// cadence span the chat. Resolved again after a profile switch replaces
// baseConfig; the old session is closed (a ballot in flight is cancelled).
// Update goroutine only (RunTurn), like jevShadow: it only builds objects,
// every oracle call runs in the background. Nil when stream_rules and the
// watchdog are both off.
func (a *TUIClientAdapter) steering() *steer.Session {
	if a.steerSet && a.steerFor == a.baseConfig {
		return a.steer
	}
	if old := a.steer; old != nil {
		go old.Close()
	}
	a.steerSet, a.steerFor = true, a.baseConfig
	o := steer.Options{Rules: a.rules, RulesMode: config.ModeShadow, Watchdog: config.ModeOff, Logf: tui.LogInfo}
	if cfg := a.baseConfig; cfg != nil {
		o.RulesMode, o.Watchdog = cfg.StreamRulesMode(), cfg.WatchdogMode()
		ws, _ := os.Getwd()
		o.Oracle = agent.WatchdogOracle(cfg, ws, tui.LogInfo)
	}
	a.steer = steer.New(o)
	return a.steer
}

// summarizer returns the small-model summarizer, built on first use.
func (a *TUIClientAdapter) summarizer() (compact.SummarizeFunc, error) {
	if a.summarize == nil {
		cfg := a.baseConfig
		if cfg == nil {
			return nil, errors.New("no configuration loaded")
		}
		a.summarize = agent.SmallModelSummarizer(llm.PlainConfigFrom(cfg), cfg.ResolveSmallModel())
	}
	return a.summarize, nil
}

var errCompactionBlocked = errors.New("compaction blocked by a PreCompact hook")

// SummarizeContext implements tui.ContextCompactor: it summarizes all but
// the newest ~20k tokens with the small-model role (#174). PreCompact runs
// inside the summarize call, which compact.Summarize only makes when there
// is something to summarize; it may block the summary or add instructions.
// PostCompact sees the summary.
func (a *TUIClientAdapter) SummarizeContext(ctx context.Context, msgs []tui.ChatMessage, focus string) (tui.SummaryOutcome, error) {
	summarize, err := a.summarizer()
	if err != nil {
		return tui.SummaryOutcome{}, err
	}
	trigger := tui.CompactionTrigger(ctx)
	blocked, fired := "", false
	hooked := func(ctx context.Context, system, user string) (string, error) {
		if blocked != "" {
			return "", errCompactionBlocked
		}
		if !fired && a.hooks.Has(hooks.EventPreCompact) {
			fired = true
			pre := a.hooks.PreCompact(ctx, trigger, focus)
			if pre.Decision != hooks.Allow {
				blocked = pre.Reason
				if blocked == "" {
					blocked = "no reason given"
				}
				return "", errCompactionBlocked
			}
			if pre.AdditionalContext != "" {
				user += "\n\nAdditional instructions from a PreCompact hook:\n" + pre.AdditionalContext
			}
		}
		return summarize(ctx, system, user)
	}
	out, res, err := compact.Summarize(ctx, msgs, compact.SummaryOptions{Focus: focus}, hooked)
	if blocked != "" {
		return tui.SummaryOutcome{}, fmt.Errorf("compaction blocked by a PreCompact hook: %s", blocked)
	}
	if errors.Is(err, compact.ErrNothingToSummarize) {
		return tui.SummaryOutcome{}, tui.ErrNothingToSummarize
	}
	if err != nil {
		return tui.SummaryOutcome{}, err
	}
	a.hooks.PostCompact(ctx, trigger, res.Summary)
	// out is the summary messages followed by the untouched tail.
	summaryLen := len(out) - (len(msgs) - res.Cut)
	return tui.SummaryOutcome{
		Cut:         res.Cut,
		Messages:    out[:summaryLen],
		Line:        res.Line(),
		TokensAfter: res.TokensAfter,
	}, nil
}

// HandoffContext implements tui.ContextHandoff: the whole history summarized
// as the opening message of a new session.
func (a *TUIClientAdapter) HandoffContext(ctx context.Context, msgs []tui.ChatMessage, focus string) (string, error) {
	summarize, err := a.summarizer()
	if err != nil {
		return "", err
	}
	_, res, err := compact.Summarize(ctx, msgs, compact.SummaryOptions{Focus: focus, All: true}, summarize)
	if err != nil {
		return "", err
	}
	return compact.HandoffText(res.Summary), nil
}

// RefreshSystemPrompt recomposes and re-injects the system prompt.
// Called after /confirm, /user, or other prompt-affecting changes.
func (a *TUIClientAdapter) RefreshSystemPrompt() {
	a.client.SetSystemPrompt(a.systemPrompt())
	tui.LogInfo("✓ System prompt refreshed (confirm/user/persona change)")
}

// runConfigCommand handles configuration commands.
func runConfigCommand(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	showConfig := fs.Bool("show", false, "Show current configuration")
	listConfigs := fs.Bool("list", false, "List all config profiles")
	setDefault := fs.Bool("set-default", false, "Make this profile the default loaded when no -config is given")
	initConfig := fs.String("init", "", "Create a new config profile (openai, grok, elevenlabs, venice, sakana, digitalocean)")
	setKey := fs.String("set-key", "", "Set API key")
	setURL := fs.String("set-url", "", "Set API URL")
	setModel := fs.String("set-model", "", "Set model")
	setMode := fs.String("set-mode", "", "Removed in 2.0 (see MIGRATING-2.0.md)")
	setMaxIter := fs.Int("set-max-tool-iterations", -1, "Set the chat's tool-loop turn cap")
	setClawMaxIterations := fs.Int("set-claw-max-iterations", -1, "Deprecated: use --set-max-tool-iterations")
	setContextLimit := fs.Int("set-context-limit", -1, "Set the context window in tokens (0 clears it and uses the model default). Required for local models, whose window celeste cannot know")
	setManagementKey := fs.String("set-management-key", "", "Set xAI Management API key for Collections")
	skipPersona := fs.String("skip-persona", "", "Removed in 2.0 (see MIGRATING-2.0.md)")
	simulateTyping := fs.String("simulate-typing", "", "Simulate typing (true/false)")
	typingSpeed := fs.Int("typing-speed", 0, "Typing speed in chars/sec (1-1000, default 60)")

	// Google Cloud authentication flags
	setGoogleCredentials := fs.String("set-google-credentials", "", "Set Google Cloud service account JSON file path")
	useGoogleADC := fs.Bool("use-google-adc", false, "Enable Google Application Default Credentials (auto-detect)")

	// Skill configuration flags
	setTarotToken := fs.String("set-tarot-token", "", "Set tarot auth token (saved to skills.json)")
	setVeniceKey := fs.String("set-venice-key", "", "Set Venice.ai API key (saved to skills.json)")
	setTarotURL := fs.String("set-tarot-url", "", "Set tarot function URL (saved to skills.json)")
	setWeatherZip := fs.String("set-weather-zip", "", "Set default weather zip code (saved to skills.json)")
	setTwitchClientID := fs.String("set-twitch-client-id", "", "Set Twitch Client ID (saved to skills.json)")
	setTwitchStreamer := fs.String("set-twitch-streamer", "", "Set default Twitch streamer (saved to skills.json)")
	setYouTubeKey := fs.String("set-youtube-key", "", "Set YouTube API key (saved to skills.json)")
	setYouTubeChannel := fs.String("set-youtube-channel", "", "Set default YouTube channel (saved to skills.json)")

	// Parse flags - exits on error due to ExitOnError flag
	_ = fs.Parse(args)

	if err := setModeError(*setMode); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if err := skipPersonaError(*skipPersona); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Handle --list
	if *listConfigs {
		configs, err := config.ListConfigs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing configs: %v\n", err)
			os.Exit(1)
		}
		defaultName := config.ResolveDefaultName() // "" when no profile is flagged
		fmt.Println("Available config profiles:")
		for _, c := range configs {
			path := config.NamedConfigPath(c)
			if c == "default" {
				path = config.NamedConfigPath("")
			}
			marker := ""
			if c == defaultName {
				marker = "  ← default (no -config needed)"
			}
			fmt.Printf("  • %s (%s)%s\n", c, path, marker)
		}
		if defaultName == "" {
			fmt.Println("\nNo profile flagged as default — bare 'config.json' is used when no -config is given.")
			fmt.Println("Set one with: celeste -config <name> config --set-default")
		}
		fmt.Println("\nUsage: celeste -config <name> chat")
		return
	}

	// Handle --set-default
	if *setDefault {
		if configName == "" {
			fmt.Fprintln(os.Stderr, "Error: --set-default needs a named profile, e.g. celeste -config sakana config --set-default")
			os.Exit(1)
		}
		if err := config.SetDefaultProfile(configName); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting default: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Default profile set to '%s'\n", configName)
		return
	}

	// Handle --init. "jev" is not a profile template: it writes the
	// TypeSafe key and turns Jev on in shadow mode in the profile below.
	jevInit := strings.EqualFold(*initConfig, "jev")
	if *initConfig != "" && !jevInit {
		if err := createConfigTemplate(*initConfig); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// No -config given: act on the flagged default profile so `config` shows and
	// edits exactly what a no-flag `celeste chat` bills against. Falls back to the
	// bare config.json when nothing is flagged.
	if configName == "" {
		if d := config.ResolveDefaultName(); d != "" {
			configName = d
			fmt.Printf("(no -config given; using default profile '%s')\n", d)
		}
	}

	// Respect the -config <name> profile flag. The default profile splits the API
	// key into secrets.json; named profiles store everything inline in
	// config.<name>.json (see SaveNamed below). If a named profile doesn't exist
	// yet, start from defaults so --set-* can create it.
	var cfg *config.Config
	var err error
	if configName == "" {
		cfg, err = config.Load()
	} else {
		cfg, err = config.LoadNamed(configName)
		// Only a MISSING profile is safe to start from defaults (so --set-* can
		// create it). A profile that exists but is corrupt/unreadable must error,
		// not silently reset — otherwise SaveNamed would overwrite the user's real
		// settings with defaults.
		if err != nil && errors.Is(err, os.ErrNotExist) {
			cfg, err = config.DefaultConfig(), nil
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = config.DefaultMaxToolIterations
	}

	changed := false

	if jevInit {
		home, _ := os.UserHomeDir()
		if err := initJev(cfg, home, keyInput(os.Stdin), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		changed = true
	}

	if *setKey != "" {
		cfg.APIKey = *setKey
		changed = true
		fmt.Println("API key updated")
	}
	if *setURL != "" {
		cfg.BaseURL = *setURL
		changed = true
		fmt.Printf("API URL set to: %s\n", *setURL)
	}
	if *setModel != "" {
		cfg.Model = *setModel
		changed = true
		fmt.Printf("Model set to: %s\n", *setModel)
	}
	if n, err := resolveMaxIterFlags(*setMaxIter, *setClawMaxIterations, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	} else if n > 0 {
		cfg.MaxToolIterations = n
		changed = true
		fmt.Printf("Max tool iterations set to: %d\n", n)
	}
	if *setContextLimit == 0 {
		cfg.ContextLimit = 0
		changed = true
		fmt.Println("Context limit cleared — using the model default")
	}
	if *setContextLimit > 0 {
		cfg.ContextLimit = *setContextLimit
		changed = true
		if maxLimit, known := config.LookupModelLimit(cfg.Model); known && *setContextLimit > maxLimit {
			fmt.Printf("Context limit set to: %d\n", cfg.ContextLimit)
			fmt.Printf("  warning: %s advertises a %d-token window; requests may be rejected\n", cfg.Model, maxLimit)
		} else {
			fmt.Printf("Context limit set to: %d\n", cfg.ContextLimit)
		}
	}
	if *setManagementKey != "" {
		cfg.XAIManagementAPIKey = *setManagementKey
		changed = true
		fmt.Println("xAI Management API key updated (for Collections)")
	}
	if *simulateTyping != "" {
		cfg.SimulateTyping = strings.ToLower(*simulateTyping) == "true"
		changed = true
		fmt.Printf("Simulate typing: %v\n", cfg.SimulateTyping)
	}
	if *typingSpeed != 0 {
		if !tui.ValidTypingSpeed(*typingSpeed) {
			fmt.Fprintf(os.Stderr, "Invalid --typing-speed %d: use 1-%d chars/sec\n", *typingSpeed, tui.MaxTypingSpeed)
			os.Exit(1)
		}
		cfg.TypingSpeed = *typingSpeed
		changed = true
		fmt.Printf("Typing speed: %d chars/sec\n", cfg.TypingSpeed)
	}

	// Handle Google Cloud authentication
	if *setGoogleCredentials != "" {
		cfg.GoogleCredentialsFile = *setGoogleCredentials
		cfg.GoogleUseADC = false
		changed = true
		fmt.Printf("✓ Google credentials file: %s\n", *setGoogleCredentials)
		fmt.Println("  Authentication will use the service account JSON file")
	}
	if *useGoogleADC {
		cfg.GoogleUseADC = true
		cfg.GoogleCredentialsFile = ""
		cfg.APIKey = "" // Clear manual API key when using ADC
		changed = true
		fmt.Println("✓ Google ADC enabled (will auto-detect credentials)")
		fmt.Println("  Run: gcloud auth application-default login")
		fmt.Println("  Or set: GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json")
	}

	// Handle skill configuration
	skillsChanged := false
	if *setTarotToken != "" {
		cfg.TarotAuthToken = *setTarotToken
		skillsChanged = true
		fmt.Println("Tarot auth token updated (saved to skills.json)")
	}
	if *setVeniceKey != "" {
		cfg.VeniceAPIKey = *setVeniceKey
		skillsChanged = true
		fmt.Println("Venice.ai API key updated (saved to skills.json)")
	}
	if *setTarotURL != "" {
		cfg.TarotFunctionURL = *setTarotURL
		skillsChanged = true
		fmt.Printf("Tarot function URL set to: %s (saved to skills.json)\n", *setTarotURL)
	}
	if *setWeatherZip != "" {
		// Validate zip code format
		zip := *setWeatherZip
		if len(zip) != 5 {
			fmt.Fprintf(os.Stderr, "Error: zip code must be 5 digits\n")
			os.Exit(1)
		}
		for _, c := range zip {
			if c < '0' || c > '9' {
				fmt.Fprintf(os.Stderr, "Error: zip code must contain only digits\n")
				os.Exit(1)
			}
		}
		cfg.WeatherDefaultZipCode = zip
		skillsChanged = true
		fmt.Printf("Default weather zip code set to: %s (saved to skills.json)\n", zip)
	}
	if *setTwitchClientID != "" {
		cfg.TwitchClientID = *setTwitchClientID
		skillsChanged = true
		fmt.Printf("Twitch Client ID set (saved to skills.json)\n")
	}
	if *setTwitchStreamer != "" {
		cfg.TwitchDefaultStreamer = *setTwitchStreamer
		skillsChanged = true
		fmt.Printf("Default Twitch streamer set to: %s (saved to skills.json)\n", *setTwitchStreamer)
	}
	if *setYouTubeKey != "" {
		cfg.YouTubeAPIKey = *setYouTubeKey
		skillsChanged = true
		fmt.Printf("YouTube API key set (saved to skills.json)\n")
	}
	if *setYouTubeChannel != "" {
		cfg.YouTubeDefaultChannel = *setYouTubeChannel
		skillsChanged = true
		fmt.Printf("Default YouTube channel set to: %s (saved to skills.json)\n", *setYouTubeChannel)
	}

	if changed {
		if configName == "" {
			// Default profile: config.json + secrets.json (key split out).
			if err := config.Save(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			if err := config.SaveSecrets(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving secrets: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Named profile: everything inline in config.<name>.json.
			if err := config.SaveNamed(configName, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config '%s': %v\n", configName, err)
				os.Exit(1)
			}
		}
		fmt.Println("Configuration saved")
	}

	if skillsChanged {
		if err := config.SaveSkillsConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving skills config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Skills configuration saved to skills.json")
	}

	if *showConfig || !changed {
		fmt.Printf("\nCurrent Configuration:\n")
		fmt.Printf("  API URL:           %s\n", cfg.BaseURL)
		fmt.Printf("  Model:             %s\n", cfg.Model)
		fmt.Printf("  API Key:           %s\n", maskKey(cfg.APIKey))
		fmt.Printf("  Simulate Typing:   %v\n", cfg.SimulateTyping)
		fmt.Printf("  Typing Speed:      %d chars/sec\n", cfg.TypingSpeed)
		if providers.OrchestratesServerSide(providers.DetectProvider(cfg.BaseURL), cfg.Model) {
			fmt.Printf("  Planning:          %s (server-side)\n", cfg.Model)
		} else {
			fmt.Printf("  Planning:          local\n")
		}
		fmt.Printf("  Max Tool Iter:     %d\n", cfg.MaxToolIterations)
		if cfg.ContextLimit > 0 {
			fmt.Printf("  Context Limit:     %d tokens (configured)\n", cfg.ContextLimit)
		} else {
			limit, known := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, 0)
			if known {
				fmt.Printf("  Context Limit:     %d tokens (model default)\n", limit)
			} else {
				fmt.Printf("  Context Limit:     %d tokens (fallback, model unknown, set --set-context-limit)\n", limit)
			}
		}
		fmt.Printf("  Venice API Key:    %s\n", maskKey(cfg.VeniceAPIKey))
		fmt.Printf("  Tarot Configured:  %v\n", cfg.TarotAuthToken != "")
		fmt.Printf("  Twitter Configured:%v\n", cfg.TwitterBearerToken != "")
		if cfg.WeatherDefaultZipCode != "" {
			fmt.Printf("  Weather Zip Code:  %s\n", cfg.WeatherDefaultZipCode)
		} else {
			fmt.Printf("  Weather Zip Code:  (not set)\n")
		}
		if cfg.TwitchClientID != "" {
			fmt.Printf("  Twitch Client ID:   %s\n", maskKey(cfg.TwitchClientID))
			if cfg.TwitchDefaultStreamer != "" {
				fmt.Printf("  Twitch Streamer:   %s\n", cfg.TwitchDefaultStreamer)
			} else {
				fmt.Printf("  Twitch Streamer:   whykusanagi (default)\n")
			}
		} else {
			fmt.Printf("  Twitch:            (not configured)\n")
		}
		if cfg.YouTubeAPIKey != "" {
			fmt.Printf("  YouTube API Key:   %s\n", maskKey(cfg.YouTubeAPIKey))
			if cfg.YouTubeDefaultChannel != "" {
				fmt.Printf("  YouTube Channel:   %s\n", cfg.YouTubeDefaultChannel)
			} else {
				fmt.Printf("  YouTube Channel:   whykusanagi (default)\n")
			}
		} else {
			fmt.Printf("  YouTube:           (not configured)\n")
		}
	}
}

// createConfigTemplate creates a config file from a template. BaseURL and model
// come from the provider registry (single source of truth) unless a template
// overrides them; only behavioural knobs that vary per provider live here.
func createConfigTemplate(name string) error {
	// override carries the per-template behavioural knobs plus any deviation from
	// the registry. provider names the registry entry to inherit BaseURL+model from;
	// baseURL/model, when set, win over the registry (for non-registry templates or
	// intentional divergence like DigitalOcean's per-agent URL).
	type override struct {
		provider string
		baseURL  string
		model    string
		timeout  int
	}
	templates := map[string]override{
		"openai":       {provider: "openai", timeout: 60},
		"grok":         {provider: "grok", timeout: 60},
		"venice":       {provider: "venice", timeout: 60},
		"sakana":       {provider: "sakana", timeout: 300}, // conductor: fan-out width is per-request, so latency is variable by design. 90s was measurably too low — a substantial prompt died at that ceiling.
		"elevenlabs":   {provider: "elevenlabs", model: "eleven_multilingual_v2", timeout: 60},
		"digitalocean": {provider: "digitalocean", baseURL: "https://your-agent.ondigitalocean.app/api/v1", timeout: 60},
	}

	if alt, ok := removedTemplates[strings.ToLower(name)]; ok {
		return fmt.Errorf("the %s template was removed in celeste 2.0 with the runtime mode; use --init %s (see MIGRATING-2.0.md)", name, alt)
	}

	o, ok := templates[strings.ToLower(name)]
	if !ok {
		return fmt.Errorf("unknown config template '%s'. Available: openai, grok, elevenlabs, venice, sakana, digitalocean", name)
	}

	caps, _ := providers.GetProvider(o.provider)
	baseURL, model := caps.BaseURL, caps.DefaultModel
	if o.baseURL != "" {
		baseURL = o.baseURL
	}
	if o.model != "" {
		model = o.model
	}
	tmpl := &config.Config{
		BaseURL:           baseURL,
		Model:             model,
		Timeout:           o.timeout,
		SimulateTyping:    true,
		TypingSpeed:       config.DefaultTypingSpeed,
		MaxToolIterations: config.DefaultMaxToolIterations,
	}

	configPath := config.NamedConfigPath(name)

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config '%s' already exists at %s", name, configPath)
	}

	// Write config
	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return err
	}

	fmt.Printf("Created config '%s' at %s\n", name, configPath)

	// Provider-specific setup instructions
	switch strings.ToLower(name) {
	case "grok":
		fmt.Println("\nSetup (xAI/Grok):")
		fmt.Println("  1. Get your API key from https://console.x.ai")
		fmt.Printf("     celeste -config %s config --set-key YOUR_XAI_KEY\n", name)
		fmt.Println("\n  2. (Optional) For Collections/RAG support, get a Management API key:")
		fmt.Printf("     celeste -config %s config --set-management-key YOUR_MGMT_KEY\n", name)
		fmt.Println("     Then: celeste collections list")
	case "openai":
		fmt.Println("\nSetup (OpenAI):")
		fmt.Println("  1. Get your API key from https://platform.openai.com/api-keys")
		fmt.Printf("     celeste -config %s config --set-key YOUR_OPENAI_KEY\n", name)
	case "venice":
		fmt.Println("\nSetup (Venice.ai):")
		fmt.Println("  1. Get your API key from https://venice.ai/settings/api")
		fmt.Printf("     celeste -config %s config --set-key YOUR_VENICE_KEY\n", name)
		fmt.Println("  2. Also set in skills.json for NSFW mode:")
		fmt.Printf("     celeste -config %s config --set-venice-key YOUR_VENICE_KEY\n", name)
	case "sakana":
		fmt.Println("\nSetup (Sakana AI / Fugu):")
		fmt.Printf("     celeste -config %s config --set-url https://api.sakana.ai/v1 --set-key YOUR_SAKANA_KEY --set-model fugu\n", name)
		fmt.Println("     (use --set-model fugu-ultra for the Ultra variant)")
	default:
		fmt.Printf("\nEdit the file to add your API key, then run:\n")
	}

	fmt.Printf("\nStart chatting:\n  celeste -config %s chat\n", name)
	return nil
}

func maskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// runSkillExecuteCommand executes a single skill from the command line.
// Usage: celeste skill <name> [--arg1 value1] [--arg2 value2]
func runSkillExecuteCommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: celeste skill <skill-name> [args...]")
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  celeste skill generate_uuid")
		fmt.Fprintln(os.Stderr, "  celeste skill get_weather --zip 90210")
		fmt.Fprintln(os.Stderr, "  celeste skill generate_password --length 20")
		fmt.Fprintln(os.Stderr, "\nUse 'celeste skills --list' to see available skills")
		os.Exit(1)
	}

	skillName := args[0]

	// Parse remaining args as key-value pairs
	skillArgs := make(map[string]any)
	for i := 1; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				value := args[i+1]

				// Try to parse as number (int or float)
				if intVal, err := strconv.Atoi(value); err == nil {
					skillArgs[key] = float64(intVal) // Use float64 for consistency with JSON numbers
				} else if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
					skillArgs[key] = floatVal
				} else {
					// Keep as string
					skillArgs[key] = value
				}

				i++ // Skip next arg since we consumed it
			} else {
				// Boolean flag
				skillArgs[key] = true
			}
		}
	}

	// Set up registry and executor
	cfg, err := config.LoadNamedWithEnv(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	registry := tools.NewRegistry()
	clAdapter := newBuiltinConfigAdapter(config.NewConfigLoader(cfg))
	execCwd, _ := os.Getwd()
	builtin.RegisterAll(registry, execCwd, clAdapter, nil, nil)
	homeDir, _ := os.UserHomeDir()
	_ = registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills"))

	// Execute skill
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toolResult, err := registry.Execute(ctx, skillName, skillArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing skill '%s': %v\n", skillName, err)
		os.Exit(1)
	}

	// Display result
	if !toolResult.Error {
		fmt.Println(toolResult.Content)
	} else {
		fmt.Fprintf(os.Stderr, "Skill '%s' failed: %s\n", skillName, toolResult.Content)
		os.Exit(1)
	}
}

// runSkillsCommand handles skill-related commands.
func runSkillsCommand(args []string) {
	fs := flag.NewFlagSet("skills", flag.ExitOnError)
	list := fs.Bool("list", false, "List available skills")
	init := fs.Bool("init", false, "Create default skill files")
	exec := fs.String("exec", "", "Execute a skill by name")
	deleteSkill := fs.String("delete", "", "Delete a skill by name")
	info := fs.String("info", "", "Show information about a skill")
	reload := fs.Bool("reload", false, "Reload skills from disk")
	// Parse flags - exits on error due to ExitOnError flag
	_ = fs.Parse(args)

	if *init {
		initHome, _ := os.UserHomeDir()
		skillsDir := filepath.Join(initHome, ".celeste", "skills")
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating skills directory: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Skills directory ready: %s\n", skillsDir)
		fmt.Println("Place custom tool JSON files here to extend Celeste.")
		return
	}

	cfg, _ := config.LoadNamed(configName)
	clAdapter := newBuiltinConfigAdapter(config.NewConfigLoader(cfg))
	skillsCwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	builtin.RegisterAll(registry, skillsCwd, clAdapter, nil, nil)
	homeDir, _ := os.UserHomeDir()
	_ = registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills"))

	// Execute skill if --exec provided
	if *exec != "" {
		// Collect remaining args after flags
		remainingArgs := fs.Args()
		allArgs := append([]string{*exec}, remainingArgs...)
		runSkillExecuteCommand(allArgs)
		return
	}

	// Handle delete subcommand
	if *deleteSkill != "" {
		// Delete the custom skill JSON file
		skillFile := filepath.Join(homeDir, ".celeste", "skills", *deleteSkill+".json")
		if err := os.Remove(skillFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting skill '%s': %v\n", *deleteSkill, err)
			os.Exit(1)
		}
		fmt.Printf("Deleted skill: %s\n", *deleteSkill)
		return
	}

	// Handle info subcommand
	if *info != "" {
		t, exists := registry.Get(*info)

		fmt.Printf("\n===================================================\n")
		fmt.Printf("           SKILL: %s\n", strings.ToUpper(*info))
		fmt.Printf("===================================================\n\n")

		if !exists {
			fmt.Printf("Status:       Not Found\n")
			fmt.Printf("\nUse 'celeste skills --list' to see available skills.\n\n")
			os.Exit(1)
		}

		fmt.Printf("Status:       Registered\n")
		fmt.Printf("Read-Only:    %v\n", t.IsReadOnly())
		fmt.Printf("\nDescription:  %s\n", t.Description())

		if t.Parameters() != nil {
			fmt.Printf("\nParameters:   (defined)\n")
		}
		fmt.Println()
		return
	}

	// Handle reload subcommand
	if *reload {
		registry = tools.NewRegistry()
		builtin.RegisterAll(registry, skillsCwd, clAdapter, nil, nil)
		_ = registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills"))
		fmt.Printf("Reloaded %d skills from disk\n", registry.Count())
		return
	}

	// Default: list skills
	if *list || len(args) == 0 {
		allTools := registry.GetAll()
		fmt.Printf("\nAvailable Skills (%d):\n", registry.Count())
		for _, t := range allTools {
			fmt.Printf("\n  %s\n", t.Name())
			fmt.Printf("    %s\n", t.Description())
		}
		fmt.Println()
	}
}

// runSessionCommand handles session-related commands.
func runSessionCommand(args []string) {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	list := fs.Bool("list", false, "List saved sessions")
	load := fs.String("load", "", "Load a session by ID")
	clear := fs.Bool("clear", false, "Clear all sessions")
	// Parse flags - exits on error due to ExitOnError flag
	_ = fs.Parse(args)

	manager := config.NewSessionManager()

	if *clear {
		if err := manager.Clear(); err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing sessions: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("All sessions cleared")
		return
	}

	if *load != "" {
		session, err := manager.Load(*load)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Loaded session: %s (%d messages)\n", session.ID, len(session.Messages))
		// In full implementation, this would resume the session in TUI
		return
	}

	if *list || len(args) == 0 {
		sessions, err := manager.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
			os.Exit(1)
		}

		if len(sessions) == 0 {
			fmt.Println("No saved sessions")
			return
		}

		fmt.Printf("\nSaved Sessions (%d):\n", len(sessions))
		for _, s := range sessions {
			summary := s.Summarize()
			fmt.Printf("\n  ID: %s\n", summary.ID)
			fmt.Printf("    Messages: %d\n", summary.MessageCount)
			fmt.Printf("    Created:  %s\n", summary.CreatedAt.Format("2006-01-02 15:04"))
			fmt.Printf("    Updated:  %s\n", summary.UpdatedAt.Format("2006-01-02 15:04"))
			if summary.FirstMessage != "" {
				fmt.Printf("    Preview:  %s\n", summary.FirstMessage)
			}
		}
		fmt.Println()
	}
}

// runCollectionsCommand handles collections-related commands.
func runCollectionsCommand(args []string) {
	// Load config
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create command
	cmd := &commands.Command{
		Name: "collections",
		Args: args,
	}

	// Execute command
	result := commands.HandleCollectionsCommand(cmd, cfg)
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if !result.Success {
		os.Exit(1)
	}
}

// runSingleMessage sends a single message and prints the response.
func runSingleMessage(message string) {
	cfg, err := config.LoadNamedWithEnv(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" && needsAPIKey(cfg) {
		fmt.Fprintln(os.Stderr, "No API key configured.")
		os.Exit(1)
	}
	resolveServedModels(cfg, os.Stderr)

	// Initialize LLM client
	client := llm.NewClient(llm.ConfigFrom(cfg), nil)

	client.SetSystemPrompt(prompts.GetSystemPrompt())

	// Send message. Cancel-only ctx; the client owns the per-attempt deadline
	// (cfg.GetTimeout()) so timeout retries get a fresh, non-expired context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messages := []tui.ChatMessage{{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}}

	result, err := client.SendMessageSync(ctx, messages, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result.Content)
}

// SessionManagerAdapter adapts config.SessionManager to tui.SessionManager interface.
type SessionManagerAdapter struct {
	manager *config.SessionManager
}

func (a *SessionManagerAdapter) NewSession() interface{} {
	return a.manager.NewSession()
}

func (a *SessionManagerAdapter) Save(session interface{}) error {
	if s, ok := session.(*config.Session); ok {
		return a.manager.Save(s)
	}
	return fmt.Errorf("invalid session type")
}

func (a *SessionManagerAdapter) Load(id string) (interface{}, error) {
	return a.manager.Load(id)
}

func (a *SessionManagerAdapter) List() ([]interface{}, error) {
	sessions, err := a.manager.List()
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(sessions))
	for i := range sessions {
		result[i] = &sessions[i]
	}
	return result, nil
}

func (a *SessionManagerAdapter) Delete(id string) error {
	return a.manager.Delete(id)
}

func (a *SessionManagerAdapter) MergeSessions(session1, session2 interface{}) interface{} {
	s1, ok1 := session1.(*config.Session)
	s2, ok2 := session2.(*config.Session)
	if !ok1 || !ok2 {
		return nil
	}
	return a.manager.MergeSessions(s1, s2)
}

// builtinConfigAdapter bridges config.ConfigLoader (returns skills.* types) to
// builtin.ConfigLoader (expects builtin.* types). The struct layouts are identical.
type builtinConfigAdapter struct {
	cl *config.ConfigLoader
}

func newBuiltinConfigAdapter(cl *config.ConfigLoader) *builtinConfigAdapter {
	return &builtinConfigAdapter{cl: cl}
}

func (a *builtinConfigAdapter) GetTarotConfig() (builtin.TarotConfig, error) {
	c, err := a.cl.GetTarotConfig()
	return builtin.TarotConfig{FunctionURL: c.FunctionURL, AuthToken: c.AuthToken}, err
}

func (a *builtinConfigAdapter) GetVeniceConfig() (builtin.VeniceConfig, error) {
	c, err := a.cl.GetVeniceConfig()
	return builtin.VeniceConfig{APIKey: c.APIKey, BaseURL: c.BaseURL, Model: c.Model, ImageModel: c.ImageModel, Upscaler: c.Upscaler}, err
}

func (a *builtinConfigAdapter) GetWeatherConfig() (builtin.WeatherConfig, error) {
	c, err := a.cl.GetWeatherConfig()
	return builtin.WeatherConfig{DefaultZipCode: c.DefaultZipCode}, err
}

func (a *builtinConfigAdapter) GetTwitchConfig() (builtin.TwitchConfig, error) {
	c, err := a.cl.GetTwitchConfig()
	return builtin.TwitchConfig{ClientID: c.ClientID, ClientSecret: c.ClientSecret, DefaultStreamer: c.DefaultStreamer}, err
}

func (a *builtinConfigAdapter) GetYouTubeConfig() (builtin.YouTubeConfig, error) {
	c, err := a.cl.GetYouTubeConfig()
	return builtin.YouTubeConfig{APIKey: c.APIKey, DefaultChannel: c.DefaultChannel}, err
}

func (a *builtinConfigAdapter) GetIPFSConfig() (builtin.IPFSConfig, error) {
	c, err := a.cl.GetIPFSConfig()
	return builtin.IPFSConfig{Provider: c.Provider, APIKey: c.APIKey, APISecret: c.APISecret, ProjectID: c.ProjectID, GatewayURL: c.GatewayURL, TimeoutSeconds: c.TimeoutSeconds}, err
}

func (a *builtinConfigAdapter) GetAlchemyConfig() (builtin.AlchemyConfig, error) {
	c, err := a.cl.GetAlchemyConfig()
	return builtin.AlchemyConfig{APIKey: c.APIKey, DefaultNetwork: c.DefaultNetwork, TimeoutSeconds: c.TimeoutSeconds}, err
}

func (a *builtinConfigAdapter) GetBlockmonConfig() (builtin.BlockmonConfig, error) {
	c, err := a.cl.GetBlockmonConfig()
	return builtin.BlockmonConfig{AlchemyAPIKey: c.AlchemyAPIKey, WebhookURL: c.WebhookURL, DefaultNetwork: c.DefaultNetwork, PollIntervalSeconds: c.PollIntervalSeconds}, err
}

func (a *builtinConfigAdapter) GetWalletSecurityConfig() (builtin.WalletSecuritySettingsConfig, error) {
	c, err := a.cl.GetWalletSecurityConfig()
	return builtin.WalletSecuritySettingsConfig{Enabled: c.Enabled, PollInterval: c.PollInterval, AlertLevel: c.AlertLevel}, err
}

// runContextCommand handles standalone context status display.
func runContextCommand(args []string) {
	// Load config to get model info
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Load most recent session
	manager := config.NewSessionManager()
	sessions, err := manager.List()
	if err != nil || len(sessions) == 0 {
		fmt.Println("No active sessions found. Start a chat to begin tracking context.")
		os.Exit(0)
	}

	// Get most recent session (sessions are sorted by UpdatedAt descending)
	session := &sessions[0]

	// Create context tracker from session
	resolved, _ := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit)
	contextTracker := config.NewContextTracker(session, cfg.Model, resolved)

	// Handle subcommand
	result := commands.HandleContextCommand(args, contextTracker)
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if !result.Success {
		os.Exit(1)
	}
}

// runStatsCommand handles standalone stats dashboard display.
func runStatsCommand(args []string) {
	// Load config
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Load most recent session
	manager := config.NewSessionManager()
	sessions, err := manager.List()
	if err != nil || len(sessions) == 0 {
		fmt.Println("No sessions found. Start a chat to generate usage statistics.")
		os.Exit(0)
	}

	// Get most recent session
	session := &sessions[0]

	// Create context tracker from session
	resolved, _ := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit)
	contextTracker := config.NewContextTracker(session, cfg.Model, resolved)

	// Generate stats output
	result := commands.HandleStatsCommand(args, contextTracker)
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if !result.Success {
		os.Exit(1)
	}
}

// runProvidersCommand handles standalone provider listing and information.
func runProvidersCommand(args []string) {
	// Load config to get current provider
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Detect current provider from BaseURL
	currentProvider := providers.DetectProvider(cfg.BaseURL)

	// Create command context
	ctx := &commands.CommandContext{
		Provider:     currentProvider,
		CurrentModel: cfg.Model,
		BaseURL:      cfg.BaseURL,
	}

	// Parse subcommand
	cmd := &commands.Command{
		Name: "providers",
		Args: args,
	}

	// Execute command
	result := commands.HandleProvidersCommand(cmd, ctx)
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if !result.Success {
		os.Exit(1)
	}
}

// runExportCommand handles standalone data export.
func runExportCommand(args []string) {
	// Load most recent session if exporting current session
	manager := config.NewSessionManager()
	sessions, err := manager.List()
	if err != nil || len(sessions) == 0 {
		fmt.Println("No sessions found to export.")
		os.Exit(0)
	}

	// Get most recent session as "current"
	session := &sessions[0]

	// Handle export
	result := commands.HandleExportCommand(args, session)
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if !result.Success {
		os.Exit(1)
	}
}

// runWalletMonitorCommand handles wallet monitoring daemon commands
func runWalletMonitorCommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: celeste wallet-monitor <start|stop|status|run>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  start   - Start the wallet monitoring daemon in the background")
		fmt.Fprintln(os.Stderr, "  stop    - Stop the running daemon")
		fmt.Fprintln(os.Stderr, "  status  - Check daemon status")
		fmt.Fprintln(os.Stderr, "  run     - Run daemon in foreground (used internally)")
		os.Exit(1)
	}

	// Load config (honors -config and the flagged default profile)
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create daemon with adapted config loader
	daemon := monitor.NewDaemon(newBuiltinConfigAdapter(config.NewConfigLoader(cfg)))

	subcommand := args[0]

	switch subcommand {
	case "start":
		if err := daemon.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
			os.Exit(1)
		}

	case "stop":
		if err := daemon.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
			os.Exit(1)
		}

	case "status":
		status, err := daemon.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wallet monitoring daemon: %s\n", status)

	case "run":
		// This is used internally when the daemon forks itself
		// The daemon package handles the actual run loop
		fmt.Fprintf(os.Stderr, "Error: 'run' command should only be called internally by daemon.Start()\n")
		os.Exit(1)

	default:
		fmt.Fprintf(os.Stderr, "Unknown wallet-monitor command: %s\n", subcommand)
		fmt.Fprintln(os.Stderr, "Valid commands: start, stop, status")
		os.Exit(1)
	}
}

// runServeCommand starts the MCP server with the given arguments.
func runServeCommand(args []string) {
	serveFlags := flag.NewFlagSet("serve", flag.ExitOnError)
	sseMode := serveFlags.Bool("sse", false, "Use SSE transport instead of stdio")
	port := serveFlags.Int("port", 8420, "Port for SSE transport")
	remote := serveFlags.Bool("remote", false, "Bind to 0.0.0.0 for network access")
	certFile := serveFlags.String("cert", "", "TLS certificate file for mTLS")
	keyFile := serveFlags.String("key", "", "TLS private key file for mTLS")
	_ = serveFlags.Parse(args)

	// A go install build upgrades in the background for the next launch;
	// MCP stdio keeps stdout (W5 ruling 28).
	newUpgradeHook().background()

	cfg, err := config.LoadNamedWithEnv(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	serverCfg := server.DefaultConfig()
	serverCfg.CelesteConfig = cfg
	serverCfg.Workspace, _ = os.Getwd()

	if *sseMode {
		serverCfg.Transport = "sse"
		serverCfg.Port = *port
		serverCfg.Remote = *remote
		serverCfg.CertFile = *certFile
		serverCfg.KeyFile = *keyFile
	}

	// Stamp the build commit so celeste_status can report which binary is
	// actually serving — a stale MCP process is otherwise indistinguishable
	// from a fresh one (both report the release-please version constant).
	server.SetBuildCommit(CommitSHA)

	srv := server.New(serverCfg)
	server.RegisterHandlers(srv)
	// Close releases the per-workspace codegraph indexers the server
	// has opened on demand. Without this, SQLite WAL files aren't
	// flushed when celeste serve exits and the next run sees stale state.
	defer srv.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Serve(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
