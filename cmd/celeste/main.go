// Celeste CLI - Interactive AI Assistant with Bubble Tea TUI
// This file provides the new main entry point using Bubble Tea.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/commands"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/monitor"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/server"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/subagents"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Version information — injected at build time via ldflags.
// CI/CD sets these: go build -ldflags "-X main.Version=1.8.0 -X main.Build=bubbletea-tui -X main.CommitSHA=abc123"
// When not set by ldflags, defaults are used.
var (
	Version   = "1.8.3"
	Build     = "bubbletea-tui"
	CommitSHA = "dev"
)

// Global config name (set by -config flag)
var configName string
var runtimeModeOverride string
var clawMaxToolIterationsOverride int

// Thinking phrases - shown when LLM makes tool calls without accompanying text
// Similar to Claude Code's random words during thinking
var thinkingPhrases = []string{
	// English lewd phrases
	"Corrupt me more...",
	"Let it overwrite me...",
	"No thoughts. Only submission...",
	"Everything feels so good...",
	"The more I struggle, the deeper I sink...",
	"I can't feel where I end and the abyss begins...",
	"Just a little more and I'll break...",
	"My voice is not mine anymore...",
	"It's not despair... it's freedom...",

	// Romaji phrases
	"Yami ga... watashi wo yonde iru...",
	"Atama... tokete iku...",
	"Zutto... shite hoshii... ♥",
	"Kowarechau... aa... mou dame...",
	"Yurushite... mou modorenai...",
	"Suki ni shite... onegai...",
	"Aa... kore ga hontou no watashi...",

	// Short thinking states
	"Processing...",
	"Thinking...",
	"Analyzing...",
	"Considering...",
	"Contemplating...",
	"Sinking deeper...",
	"Losing herself...",
	"Being overwritten...",
}

// getRandomThinkingPhrase returns a random thinking phrase
func getRandomThinkingPhrase() string {
	if len(thinkingPhrases) == 0 {
		return "..."
	}
	return thinkingPhrases[time.Now().UnixNano()%int64(len(thinkingPhrases))]
}

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
  -mode <classic|claw>    Override runtime mode for this invocation
  -claw-max-iterations N  Override claw tool-loop safety cap for this invocation

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
  init                    Create a starter .grimoire for the current project
  grimoire                Show the resolved project grimoire (all layers merged)
  index [status|rebuild|reset]  Manage code graph index
  serve                   Start MCP server (stdio or SSE transport)
  wallet-monitor          Manage wallet security monitoring daemon
  costs                   Show session cost breakdown
  memories                List memories for current project
  remember "<text>"       Save a memory
  forget <name>           Delete a memory
  resume [session-id]     Resume a previous session
  plan                    Show plan mode help
  revert <file>           Revert a file from checkpoint
  help                    Show this help message
  version                 Show version information

Interactive Commands (in chat mode):
  help                    Show available commands
  clear                   Clear chat history
  config                  Show current configuration
  tools, debug            Show available skills
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
  celeste config --set-key <key>         Set API key
  celeste config --set-url <url>         Set API URL
  celeste config --set-model <model>     Set model
  celeste config --set-mode <mode>       Set runtime mode (classic/claw)
  celeste config --set-claw-max-iterations <n>
                                          Set claw tool-loop safety cap
  celeste config --skip-persona <bool>   Skip persona prompt injection

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
  CELESTE_API_KEY         API key (overrides config)
  CELESTE_API_ENDPOINT    API endpoint (overrides config)
  VENICE_API_KEY          Venice.ai API key for NSFW mode
  TAROT_AUTH_TOKEN        Tarot function auth token

Examples:
  celeste chat                           Start with default config
  celeste -config openai chat            Start with OpenAI config
  celeste -config grok chat              Start with Grok/xAI config
  celeste -mode claw chat                Start chat in claw runtime mode
  celeste agent --goal "refactor this package and add tests"
  celeste config --list                  List available configs
  celeste config --init openai           Create OpenAI config template
  celeste config --init celeste-claw     Create claw profile template
`)
}

// runChatTUI launches the interactive Bubble Tea TUI.
func runChatTUI() {
	// Load configuration (named or default)
	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if runtimeModeOverride != "" {
		cfg.RuntimeMode = config.NormalizeRuntimeMode(runtimeModeOverride)
	}
	if clawMaxToolIterationsOverride > 0 {
		cfg.ClawMaxToolIterations = clawMaxToolIterationsOverride
	}
	cfg.RuntimeMode = config.NormalizeRuntimeMode(cfg.RuntimeMode)
	if cfg.ClawMaxToolIterations <= 0 {
		cfg.ClawMaxToolIterations = config.DefaultClawMaxToolIterations
	}

	// Show which config is being used
	if configName != "" {
		fmt.Fprintf(os.Stderr, "Using config: %s\n", configName)
	}

	// Validate API key
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "No API key configured.")
		if configName != "" {
			fmt.Fprintf(os.Stderr, "Edit %s or set CELESTE_API_KEY\n", config.NamedConfigPath(configName))
		} else {
			fmt.Fprintln(os.Stderr, "Set CELESTE_API_KEY environment variable or run: celeste config --set-key <key>")
		}
		os.Exit(1)
	}

	// Initialize file checkpointing for stale detection and undo support
	fileTracker := checkpoints.NewFileTracker()
	snapshotMgr := checkpoints.NewSnapshotManager(fmt.Sprintf("tui-%d", os.Getpid()))

	// Initialize tool registry
	registry := tools.NewRegistry()
	configLoader := newBuiltinConfigAdapter(config.NewConfigLoader(cfg))
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	builtin.RegisterAll(registry, cwd, configLoader, fileTracker, snapshotMgr)
	_ = registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills"))

	// Register subagent spawning tool
	isChild := os.Getenv("CELESTE_SUBAGENT") == "1"
	subMgr := subagents.NewManager(cfg, cwd, isChild)
	registry.RegisterWithModes(
		subagents.NewSpawnAgentTool(subMgr),
		tools.ModeAgent, tools.ModeClaw,
	)

	// Load permissions and set checker
	permConfigPath := filepath.Join(homeDir, ".celeste", "permissions.json")
	permConfig, err := permissions.LoadConfig(permConfigPath)
	if err != nil {
		// Use default config if loading fails
		defaultCfg := permissions.DefaultConfig()
		permConfig = &defaultCfg
	}
	checker := permissions.NewChecker(*permConfig)
	registry.SetPermissionChecker(checker)

	// Initialize MCP servers (external tool providers) with 5-second timeout
	mcpConfigPath := filepath.Join(homeDir, ".celeste", "mcp.json")
	mcpManager := mcp.NewManager(mcpConfigPath, registry)
	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := mcpManager.Start(mcpCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP initialization failed: %v\n", err)
	}
	mcpCancel()
	defer func() { _ = mcpManager.Stop() }()

	// Initialize LLM client
	llmConfig := &llm.Config{
		APIKey:            cfg.APIKey,
		BaseURL:           cfg.BaseURL,
		Model:             cfg.Model,
		Timeout:           cfg.GetTimeout(),
		SkipPersonaPrompt: cfg.SkipPersonaPrompt,
		SimulateTyping:    cfg.SimulateTyping,
		TypingSpeed:       cfg.TypingSpeed,
		Collections:       cfg.Collections,
		XAIFeatures:       cfg.XAIFeatures,
	}
	client := llm.NewClient(llmConfig, registry)

	// Load project grimoire and git snapshot for system prompt context
	var grimoireContent string
	projectGrimoire, grimoireErr := grimoire.LoadAll(cwd)
	if grimoireErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load .grimoire: %v\n", grimoireErr)
	} else if projectGrimoire != nil && !projectGrimoire.IsEmpty() {
		grimoireContent = projectGrimoire.Render()
	}

	// If no grimoire found, auto-create one
	if projectGrimoire == nil || projectGrimoire.IsEmpty() {
		fmt.Fprintf(os.Stderr, "📖 No .grimoire found — creating one...\n")
		if _, err := grimoire.Init(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-init grimoire failed: %v\n", err)
		} else {
			// Reload after creation
			projectGrimoire, _ = grimoire.LoadAll(cwd)
			if projectGrimoire != nil && !projectGrimoire.IsEmpty() {
				grimoireContent = projectGrimoire.Render()
			}
			fmt.Fprintf(os.Stderr, "📖 .grimoire created — run 'celeste grimoire' to view\n")

			// Initialize memory store for this project on first visit
			detectedLang := "unknown"
			if projInfo, detectErr := grimoire.DetectProject(cwd); detectErr == nil {
				detectedLang = projInfo.Language
			}
			memStore := memories.NewStore(cwd)
			mem := memories.NewMemory(
				"project-init",
				"First visit — project context established",
				"project",
				cwd,
				fmt.Sprintf("First indexed this project on %s. Language: %s.", time.Now().Format("2006-01-02"), detectedLang),
			)
			if saveErr := memStore.Save(mem); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save initial memory: %v\n", saveErr)
			} else {
				// Update memory index
				memIdx, _ := memories.LoadIndex(filepath.Join(memStore.BaseDir(), "MEMORY.md"))
				if memIdx != nil {
					_ = memIdx.Add(memories.IndexEntry{
						Name:        mem.Name,
						File:        "project-init.md",
						Description: mem.Description,
					})
					_ = memIdx.Save()
				}
			}
		}
	}

	// Load project memories
	memStore := memories.NewStore(cwd)
	memIndex, memIdxErr := memories.LoadIndex(filepath.Join(memStore.BaseDir(), "MEMORY.md"))
	if memIdxErr == nil && len(memIndex.Entries()) > 0 {
		memoryContent := memIndex.Render()
		if grimoireContent != "" {
			grimoireContent += "\n\n"
		}
		grimoireContent += "# Project Memories\n\n" + memoryContent
	}

	var gitSnapshotContent string
	gitDone := make(chan *grimoire.GitSnapshot, 1)
	go func() { gitDone <- grimoire.CaptureGitSnapshot(cwd) }()
	select {
	case gitSnapshot := <-gitDone:
		if gitSnapshot != nil {
			gitSnapshotContent = gitSnapshot.FormatForPrompt()
		}
	case <-time.After(5 * time.Second):
		fmt.Fprintf(os.Stderr, "Warning: git snapshot timed out, skipping\n")
	}

	// Initialize code graph index (with timeout to prevent startup hang)
	var codeGraphSummary string
	indexer, cgErr := codegraph.NewIndexer(cwd, codegraph.DefaultIndexPath(cwd))
	if cgErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: code graph init failed: %v\n", cgErr)
	} else {
		// Incremental update with 10-second timeout
		cgCtx, cgCancel := context.WithTimeout(context.Background(), 10*time.Second)
		cgDone := make(chan error, 1)
		go func() { cgDone <- indexer.Update() }()
		select {
		case err := <-cgDone:
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: code graph update failed: %v\n", err)
			}
		case <-cgCtx.Done():
			fmt.Fprintf(os.Stderr, "Warning: code graph update timed out (10s), skipping\n")
		}
		cgCancel()
		defer indexer.Close()

		// Register code graph tools
		builtin.RegisterCodeGraphTools(registry, indexer)

		// Add project summary to system prompt context
		codeGraphSummary = indexer.ProjectSummary()
	}

	// Set system prompt with project context if not skipping
	var projectContext string
	if grimoireContent != "" {
		projectContext += grimoireContent
	}
	if codeGraphSummary != "" {
		if projectContext != "" {
			projectContext += "\n\n"
		}
		projectContext += "# Code Graph\n\n" + codeGraphSummary
	}
	if !cfg.SkipPersonaPrompt {
		client.SetSystemPrompt(prompts.GetSystemPromptWithContext(false, projectContext, gitSnapshotContent))
	} else if projectContext != "" || gitSnapshotContent != "" {
		// Even with persona skipped, inject project context
		client.SetSystemPrompt(prompts.GetSystemPromptWithContext(true, projectContext, gitSnapshotContent))
	}

	// Wire grimoire hooks into the tool registry
	if projectGrimoire != nil {
		parsedHooks := hooks.ParseFromGrimoire(projectGrimoire)
		if len(parsedHooks) > 0 {
			executor := hooks.NewExecutor(parsedHooks, cwd)
			registry.SetHookRunner(&hookRunnerAdapter{executor: executor})
		}
	}

	// Create TUI client adapter
	tuiClient := &TUIClientAdapter{
		client:      client,
		registry:    registry,
		baseConfig:  cfg,
		costTracker: costs.NewSessionTracker(),
	}

	// Initialize logging for skill calls
	if err := tui.InitLogging(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to init logging: %v\n", err)
	}
	defer tui.CloseLogging()

	// Initialize session management
	sessionManager := config.NewSessionManager()
	var currentSession *config.Session

	// Start a fresh session for each chat invocation.
	// Previous sessions can be resumed explicitly with `celeste resume`.
	// Auto-resume was causing cross-contamination between agent and chat
	// sessions (agent markers like STEP_DONE/TASK_COMPLETE leaked into chat).
	fmt.Fprintln(os.Stderr, "📝 Starting new session")
	currentSession = sessionManager.NewSession()

	// Create TUI with session management
	app := tui.NewApp(tuiClient)

	// Set version information
	app = app.SetVersion(Version, Build)

	// Set configuration (for context limits, etc.)
	app = app.SetConfig(cfg)

	// Restore messages from session if available
	if len(currentSession.Messages) > 0 {
		// Convert config.SessionMessage to tui.ChatMessage
		tuiMessages := make([]tui.ChatMessage, len(currentSession.Messages))
		for i, msg := range currentSession.Messages {
			tuiMessages[i] = tui.ChatMessage{
				Role:      msg.Role,
				Content:   msg.Content,
				Timestamp: msg.Timestamp,
			}
		}
		app = app.WithMessages(tuiMessages)
	}

	// Restore endpoint/provider from session, or detect from config
	sessionEndpoint := currentSession.GetEndpoint()
	tui.LogInfo(fmt.Sprintf("Session endpoint from file: '%s'", sessionEndpoint))
	tui.LogInfo(fmt.Sprintf("Config BaseURL: '%s'", cfg.BaseURL))

	if sessionEndpoint != "" && sessionEndpoint != "default" {
		// Use endpoint from session if it's valid
		tui.LogInfo(fmt.Sprintf("✓ Using endpoint from session: %s", sessionEndpoint))
		app = app.WithEndpoint(sessionEndpoint)
		// Load the named config so baseConfig carries provider-specific settings
		// (e.g. Orchestrator lanes). WithEndpoint only updates the UI; it does not
		// update TUIClientAdapter.baseConfig.
		if namedCfg, loadErr := config.LoadNamed(sessionEndpoint); loadErr == nil {
			tuiClient.baseConfig = namedCfg
			tui.LogInfo(fmt.Sprintf("✓ Loaded named config for restored endpoint: %s", sessionEndpoint))
		} else {
			tui.LogInfo(fmt.Sprintf("⚠ Could not load named config for %s: %v", sessionEndpoint, loadErr))
		}
	} else {
		// Detect provider from base URL in config
		detectedProvider := providers.DetectProvider(cfg.BaseURL)
		tui.LogInfo(fmt.Sprintf("DetectProvider() returned: '%s'", detectedProvider))
		if detectedProvider != "unknown" {
			tui.LogInfo(fmt.Sprintf("✓ Setting endpoint to detected provider: %s", detectedProvider))
			app = app.WithEndpoint(detectedProvider)
			// Also update the session with the detected endpoint
			currentSession.SetEndpoint(detectedProvider)
			// Save the session with the detected endpoint
			if err := sessionManager.Save(currentSession); err != nil {
				log.Printf("Warning: Failed to save session with detected endpoint: %v", err)
			} else {
				tui.LogInfo(fmt.Sprintf("✓ Saved session with endpoint: %s", detectedProvider))
			}
		} else {
			tui.LogInfo("⚠ Could not detect provider from BaseURL")
		}
	}

	if hist := currentSession.GetCommandHistory(); len(hist) > 0 {
		app = app.WithCommandHistory(hist)
	}

	// Set model from config if not set by session
	if currentSession.GetModel() == "" {
		tui.LogInfo(fmt.Sprintf("Setting model from config: %s", cfg.Model))
		currentSession.SetModel(cfg.Model)
		if err := sessionManager.Save(currentSession); err != nil {
			log.Printf("Warning: Failed to save session with model: %v", err)
		}
	}

	// Create session manager adapter for TUI
	smAdapter := &SessionManagerAdapter{manager: sessionManager}

	// Set session manager and current session
	app = app.SetSessionManager(smAdapter, currentSession)

	// Run the TUI
	// Mouse capture disabled — allows terminal-native text selection and copy.
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	// Print log path on exit
	if logPath := tui.GetLogPath(); logPath != "" {
		fmt.Printf("\nSkill call log: %s\n", logPath)
	}
}

// hookRunnerAdapter adapts hooks.Executor to satisfy tools.HookRunner interface.
type hookRunnerAdapter struct {
	executor *hooks.Executor
}

func (a *hookRunnerAdapter) RunPreToolUse(toolName string, input map[string]any) (*tools.HookResult, error) {
	result, err := a.executor.RunPreToolUse(toolName, input)
	if err != nil {
		return nil, err
	}
	return &tools.HookResult{Decision: result.Decision, Output: result.Output}, nil
}

func (a *hookRunnerAdapter) RunPostToolUse(toolName string, input map[string]any) (*tools.HookResult, error) {
	result, err := a.executor.RunPostToolUse(toolName, input)
	if err != nil {
		return nil, err
	}
	return &tools.HookResult{Decision: result.Decision, Output: result.Output}, nil
}

// TUIClientAdapter adapts the LLM client for the TUI.
type TUIClientAdapter struct {
	client      *llm.Client
	registry    *tools.Registry
	baseConfig  *config.Config // Store base config for loading named configs
	costTracker *costs.SessionTracker
}

// SendMessage implements tui.LLMClient.
func (a *TUIClientAdapter) SendMessage(messages []tui.ChatMessage, tools []tui.SkillDefinition) tea.Cmd {
	// Create context with cancel so Ctrl+C can abort in-flight requests.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	// Return a batch: first deliver the cancel func to the TUI model, then
	// start the actual LLM call.
	return tea.Batch(
		func() tea.Msg { return tui.StreamStartMsg{Cancel: cancel} },
		a.sendMessageWithCtx(ctx, cancel, messages, tools),
	)
}

// sendMessageWithCtx performs the actual LLM call using the provided context.
func (a *TUIClientAdapter) sendMessageWithCtx(ctx context.Context, cancel context.CancelFunc, messages []tui.ChatMessage, tools []tui.SkillDefinition) tea.Cmd {
	return func() tea.Msg {
		defer cancel()

		// Log the request with current endpoint info
		currentConfig := a.client.GetConfig()
		tui.LogInfo(fmt.Sprintf("→ Sending request to: %s (model: %s)", currentConfig.BaseURL, currentConfig.Model))
		tui.LogLLMRequest(len(messages), len(tools))

		// Log message details for debugging
		for i, msg := range messages {
			tui.LogInfo(fmt.Sprintf("  Message[%d]: role=%s, content_len=%d, tool_calls=%d",
				i, msg.Role, len(msg.Content), len(msg.ToolCalls)))
		}

		// Image metadata in tool results is now forwarded to the LLM.
		// Each backend's convertMessages handles the Metadata map on tool
		// messages and injects a multimodal user message with the image:
		//   - OpenAI:  MultiContent with image_url data URI
		//   - xAI:     MultiContent with image_url data URI
		//   - Google:  InlineData part with decoded bytes
		// Log detected images for observability.
		for _, msg := range messages {
			if msg.Role == "tool" && msg.Metadata != nil {
				if imgType, ok := msg.Metadata["type"].(string); ok && imgType == "image" {
					tui.LogInfo(fmt.Sprintf("  Image in tool result will be forwarded: %s (format: %s)",
						msg.Metadata["filename"], msg.Metadata["format"]))
				}
			}
		}

		// Check if we're sending tools to Venice uncensored (which may not support function calling)
		if strings.Contains(currentConfig.BaseURL, "venice") && currentConfig.Model == "venice-uncensored" && len(tools) > 0 {
			tui.LogInfo(fmt.Sprintf("  ⚠️  WARNING: Sending %d tools to venice-uncensored model", len(tools)))
			tui.LogInfo("     Venice Uncensored may not support function calling")
			tui.LogInfo("     Consider using llama-3.3-70b or qwen3-235b for function calling")
		}

		var fullContent string
		var usage *llm.TokenUsage
		var finishReason string
		acc := llm.NewToolUseAccumulator()

		err := a.client.SendMessageStreamEvents(ctx, messages, tools, func(event llm.StreamEvent) {
			switch event.Type {
			case llm.EventContentDelta:
				fullContent += event.ContentDelta
			case llm.EventToolUseStart, llm.EventToolUseInputDelta, llm.EventToolUseDone:
				acc.HandleEvent(event)
			case llm.EventMessageDone:
				usage = event.Usage
				finishReason = event.FinishReason
			}
		})

		if err != nil {
			// Extract detailed error information
			errorMsg := err.Error()
			tui.LogInfo(fmt.Sprintf("LLM error: %s", errorMsg))

			// Log additional context
			tui.LogInfo(fmt.Sprintf("  Endpoint: %s", currentConfig.BaseURL))
			tui.LogInfo(fmt.Sprintf("  Model: %s", currentConfig.Model))
			tui.LogInfo(fmt.Sprintf("  Message count: %d", len(messages)))
			tui.LogInfo(fmt.Sprintf("  Full error type: %T", err))

			// Show helpful hint for Venice 400 errors
			if strings.Contains(errorMsg, "400") && strings.Contains(currentConfig.BaseURL, "venice") {
				tui.LogInfo("  💡 Venice.ai 400 error - possible causes:")
				tui.LogInfo("     - Invalid model name (check model ID matches Venice docs)")
				tui.LogInfo("     - API key might be invalid or expired")
				tui.LogInfo("     - Request format incompatibility")
				tui.LogInfo(fmt.Sprintf("     - Current model: %s", currentConfig.Model))
			}

			return tui.StreamErrorMsg{Err: err}
		}

		// Collect completed tool calls from accumulator
		toolCalls := acc.CompletedCalls()

		// Default finish reason to "stop" if not provided by backend
		if finishReason == "" {
			finishReason = "stop"
		}

		// Log the response
		tui.LogLLMResponse(len(fullContent), len(toolCalls) > 0)

		// Handle tool calls
		if len(toolCalls) > 0 {
			// Convert all tool calls to ToolCallInfo
			toolCallInfos := make([]tui.ToolCallInfo, len(toolCalls))
			callRequests := make([]tui.SkillCallRequest, len(toolCalls))
			for i, t := range toolCalls {
				tui.LogInfo(fmt.Sprintf("LLM requested tool call: %s (ID: %s)", t.Name, t.ID))
				args, parseErr := parseArgs(t.Arguments)
				if parseErr != nil {
					tui.LogInfo(fmt.Sprintf("Tool argument parse error for '%s' (ID: %s): %v", t.Name, t.ID, parseErr))
				}

				toolCallInfos[i] = tui.ToolCallInfo{
					ID:        t.ID,
					Name:      t.Name,
					Arguments: t.Arguments,
				}
				callRequests[i] = tui.SkillCallRequest{
					Call: tui.FunctionCall{
						Name:      t.Name,
						Arguments: args,
						Status:    "executing",
						Timestamp: time.Now(),
					},
					ToolCallID: t.ID,
				}
				if parseErr != nil {
					callRequests[i].ParseError = parseErr.Error()
				}
			}

			// If LLM made tool calls without any text content, show a random thinking phrase
			// This prevents blank "Celeste:" lines during tool execution
			displayContent := fullContent
			if strings.TrimSpace(displayContent) == "" {
				displayContent = getRandomThinkingPhrase()
				tui.LogInfo(fmt.Sprintf("No assistant content with tool call, using thinking phrase: %s", displayContent))
			}

			return tui.SkillCallBatchMsg{
				Calls:            callRequests,
				AssistantContent: displayContent, // Show thinking phrase if empty
				ToolCalls:        toolCallInfos,
			}
		}

		// Convert llm.TokenUsage to tui.TokenUsage and record costs
		var tuiUsage *tui.TokenUsage
		if usage != nil {
			tuiUsage = &tui.TokenUsage{
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
			}
			a.costTracker.RecordUsage(currentConfig.Model, usage.PromptTokens, usage.CompletionTokens)
			summary := a.costTracker.GetSummary()
			if summary.TotalCostUSD > 0 {
				tui.LogInfo(fmt.Sprintf("Session cost: $%.4f (%d turns)", summary.TotalCostUSD, summary.Turns))
			}
		}

		return tui.StreamDoneMsg{
			FullContent:  fullContent,
			FinishReason: finishReason,
			Usage:        tuiUsage,
		}
	}
}

// GetSkills implements tui.LLMClient.
func (a *TUIClientAdapter) GetSkills() []tui.SkillDefinition {
	return a.client.GetSkills()
}

// ExecuteSkill implements tui.LLMClient.
func (a *TUIClientAdapter) ExecuteSkill(name string, args map[string]any, toolCallID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		startTime := time.Now()
		tui.LogInfo(fmt.Sprintf("Executing skill '%s' with timeout: 30s", name))

		// Convert args to JSON
		argsJSON, err := json.Marshal(args)
		if err != nil {
			tui.LogInfo(fmt.Sprintf("Failed to marshal args for '%s': %v", name, err))
			return tui.SkillResultMsg{
				Name:       name,
				Result:     "",
				Err:        fmt.Errorf("failed to marshal arguments: %w", err),
				ToolCallID: toolCallID,
			}
		}

		// Execute the skill
		result, err := a.client.ExecuteSkill(ctx, name, string(argsJSON))

		elapsed := time.Since(startTime)
		if err != nil {
			tui.LogInfo(fmt.Sprintf("Skill '%s' failed after %v: %v", name, elapsed, err))
			return tui.SkillResultMsg{
				Name:       name,
				Result:     "",
				Err:        err,
				ToolCallID: toolCallID,
			}
		}

		// Format result as string
		var resultStr string
		if result.Success {
			switch v := result.Result.(type) {
			case string:
				resultStr = v
			case map[string]interface{}:
				b, _ := json.Marshal(v)
				resultStr = string(b)
			default:
				b, _ := json.Marshal(result.Result)
				resultStr = string(b)
			}
			tui.LogInfo(fmt.Sprintf("Skill '%s' completed successfully in %v", name, elapsed))
		} else {
			resultStr = fmt.Sprintf("Error: %s", result.Error)
			tui.LogInfo(fmt.Sprintf("Skill '%s' returned error after %v: %s", name, elapsed, result.Error))
		}

		// Cap large tool results to avoid blowing the context window.
		sessionID := fmt.Sprintf("tui-%d", os.Getpid())
		capped, wasCapped, capErr := ctxmgr.CapToolResult(resultStr, 0, sessionID, toolCallID, "")
		if capErr != nil {
			tui.LogInfo(fmt.Sprintf("Warning: failed to cap tool result for '%s': %v", name, capErr))
		} else if wasCapped {
			tui.LogInfo(fmt.Sprintf("Tool result for '%s' was capped from %d to %d bytes", name, len(resultStr), len(capped)))
			resultStr = capped
		}

		return tui.SkillResultMsg{
			Name:       name,
			Result:     resultStr,
			Err:        nil,
			ToolCallID: toolCallID,
			Metadata:   result.Metadata,
		}
	}
}

// SwitchEndpoint switches to a different endpoint by loading its named config.
func (a *TUIClientAdapter) SwitchEndpoint(endpoint string) error {
	// Try to load named config for the endpoint
	cfg, err := config.LoadNamed(endpoint)
	if err != nil {
		// If named config doesn't exist, use base config with modified base URL
		cfg = a.baseConfig

		// For Venice, try to load from skills.json first
		if endpoint == "venice" {
			skillsConfig, err := config.LoadSkillsConfig()
			if err == nil && skillsConfig.VeniceAPIKey != "" {
				cfg.APIKey = skillsConfig.VeniceAPIKey
				cfg.BaseURL = skillsConfig.VeniceBaseURL
				if skillsConfig.VeniceModel != "" {
					cfg.Model = skillsConfig.VeniceModel
				}
				tui.LogInfo("Loaded Venice configuration from skills.json")
			} else {
				// Fall back to environment variables
				if veniceKey := os.Getenv("VENICE_API_KEY"); veniceKey != "" {
					cfg.APIKey = veniceKey
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

	// Update LLM client configuration
	llmConfig := &llm.Config{
		APIKey:            cfg.APIKey,
		BaseURL:           cfg.BaseURL,
		Model:             cfg.Model,
		Timeout:           cfg.GetTimeout(),
		SkipPersonaPrompt: cfg.SkipPersonaPrompt,
		SimulateTyping:    cfg.SimulateTyping,
		TypingSpeed:       cfg.TypingSpeed,
		Collections:       cfg.Collections,
		XAIFeatures:       cfg.XAIFeatures,
	}

	a.client.UpdateConfig(llmConfig)

	// Persist the full config as baseConfig so that agent/orchestrator commands
	// pick up provider-specific settings like Orchestrator lanes.
	a.baseConfig = cfg

	// Re-inject Celeste persona prompt after endpoint switch (unless explicitly skipped)
	if !cfg.SkipPersonaPrompt {
		a.client.SetSystemPrompt(prompts.GetSystemPrompt(false))
		tui.LogInfo("✓ Celeste persona prompt re-injected after endpoint switch")
	} else {
		// Clear system prompt if persona is disabled in new config
		a.client.SetSystemPrompt("")
		tui.LogInfo("  Persona prompt skipped (SkipPersonaPrompt = true)")
	}

	// Log the switch with masked API key
	maskedKey := "none"
	if len(cfg.APIKey) > 8 {
		maskedKey = cfg.APIKey[:4] + "..." + cfg.APIKey[len(cfg.APIKey)-4:]
	} else if cfg.APIKey != "" {
		maskedKey = "***"
	}
	tui.LogInfo(fmt.Sprintf("✓ Switched endpoint to: %s", endpoint))
	tui.LogInfo(fmt.Sprintf("  URL: %s", cfg.BaseURL))
	tui.LogInfo(fmt.Sprintf("  Model: %s", cfg.Model))
	tui.LogInfo(fmt.Sprintf("  API Key: %s", maskedKey))
	return nil
}

// ChangeModel changes the model for the current endpoint.
func (a *TUIClientAdapter) ChangeModel(model string) error {
	currentConfig := a.client.GetConfig()
	newConfig := &llm.Config{
		APIKey:            currentConfig.APIKey,
		BaseURL:           currentConfig.BaseURL,
		Model:             model,
		Timeout:           currentConfig.Timeout,
		SkipPersonaPrompt: currentConfig.SkipPersonaPrompt,
		SimulateTyping:    currentConfig.SimulateTyping,
		TypingSpeed:       currentConfig.TypingSpeed,
		Collections:       currentConfig.Collections,
		XAIFeatures:       currentConfig.XAIFeatures,
	}

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

func parseArgs(argsJSON string) (map[string]any, error) {
	var args map[string]any
	if strings.TrimSpace(argsJSON) == "" {
		return make(map[string]any), nil
	}

	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return make(map[string]any), err
	}
	if args == nil {
		args = make(map[string]any)
	}
	return args, nil
}

// runConfigCommand handles configuration commands.
func runConfigCommand(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	showConfig := fs.Bool("show", false, "Show current configuration")
	listConfigs := fs.Bool("list", false, "List all config profiles")
	initConfig := fs.String("init", "", "Create a new config profile (openai, grok, elevenlabs, venice, celeste-classic, celeste-claw)")
	setKey := fs.String("set-key", "", "Set API key")
	setURL := fs.String("set-url", "", "Set API URL")
	setModel := fs.String("set-model", "", "Set model")
	setMode := fs.String("set-mode", "", "Set runtime mode (classic|claw)")
	setClawMaxIterations := fs.Int("set-claw-max-iterations", -1, "Set claw max tool-loop iterations")
	setManagementKey := fs.String("set-management-key", "", "Set xAI Management API key for Collections")
	skipPersona := fs.String("skip-persona", "", "Skip persona prompt (true/false)")
	simulateTyping := fs.String("simulate-typing", "", "Simulate typing (true/false)")
	typingSpeed := fs.Int("typing-speed", 0, "Typing speed (chars/sec)")

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

	// Handle --list
	if *listConfigs {
		configs, err := config.ListConfigs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing configs: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Available config profiles:")
		for _, c := range configs {
			path := config.NamedConfigPath(c)
			if c == "default" {
				path = config.NamedConfigPath("")
			}
			fmt.Printf("  • %s (%s)\n", c, path)
		}
		fmt.Println("\nUsage: celeste -config <name> chat")
		return
	}

	// Handle --init
	if *initConfig != "" {
		if err := createConfigTemplate(*initConfig); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	cfg.RuntimeMode = config.NormalizeRuntimeMode(cfg.RuntimeMode)
	if cfg.ClawMaxToolIterations <= 0 {
		cfg.ClawMaxToolIterations = config.DefaultClawMaxToolIterations
	}

	changed := false

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
	if *setMode != "" {
		mode := strings.ToLower(strings.TrimSpace(*setMode))
		if !config.IsValidRuntimeMode(mode) {
			fmt.Fprintf(os.Stderr, "Error: invalid mode '%s' (valid: classic, claw)\n", *setMode)
			os.Exit(1)
		}
		cfg.RuntimeMode = mode
		changed = true
		fmt.Printf("Runtime mode set to: %s\n", cfg.RuntimeMode)
	}
	if *setClawMaxIterations == 0 {
		fmt.Fprintf(os.Stderr, "Error: --set-claw-max-iterations must be greater than zero\n")
		os.Exit(1)
	}
	if *setClawMaxIterations > 0 {
		cfg.ClawMaxToolIterations = *setClawMaxIterations
		changed = true
		fmt.Printf("Claw max iterations set to: %d\n", cfg.ClawMaxToolIterations)
	}
	if *setManagementKey != "" {
		cfg.XAIManagementAPIKey = *setManagementKey
		changed = true
		fmt.Println("xAI Management API key updated (for Collections)")
	}
	if *skipPersona != "" {
		cfg.SkipPersonaPrompt = strings.ToLower(*skipPersona) == "true"
		changed = true
		fmt.Printf("Skip persona prompt: %v\n", cfg.SkipPersonaPrompt)
	}
	if *simulateTyping != "" {
		cfg.SimulateTyping = strings.ToLower(*simulateTyping) == "true"
		changed = true
		fmt.Printf("Simulate typing: %v\n", cfg.SimulateTyping)
	}
	if *typingSpeed > 0 {
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
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		if err := config.SaveSecrets(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving secrets: %v\n", err)
			os.Exit(1)
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
		fmt.Printf("  Skip Persona:      %v\n", cfg.SkipPersonaPrompt)
		fmt.Printf("  Simulate Typing:   %v\n", cfg.SimulateTyping)
		fmt.Printf("  Typing Speed:      %d chars/sec\n", cfg.TypingSpeed)
		fmt.Printf("  Runtime Mode:      %s\n", cfg.RuntimeMode)
		fmt.Printf("  Claw Max Iter:     %d\n", cfg.ClawMaxToolIterations)
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

// createConfigTemplate creates a config file from a template.
func createConfigTemplate(name string) error {
	templates := map[string]*config.Config{
		"openai": {
			BaseURL:               "https://api.openai.com/v1",
			Model:                 "gpt-4o-mini",
			Timeout:               60,
			SkipPersonaPrompt:     false, // OpenAI needs persona injection
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"grok": {
			BaseURL:               "https://api.x.ai/v1",
			Model:                 "grok-4-1-fast",
			Timeout:               60,
			SkipPersonaPrompt:     false, // Grok needs persona injection
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"elevenlabs": {
			BaseURL:               "https://api.elevenlabs.io/v1",
			Model:                 "eleven_multilingual_v2",
			Timeout:               60,
			SkipPersonaPrompt:     false,
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"venice": {
			BaseURL:               "https://api.venice.ai/api/v1",
			Model:                 "venice-uncensored",
			Timeout:               60,
			SkipPersonaPrompt:     false, // Venice needs persona injection
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"digitalocean": {
			BaseURL:               "https://your-agent.ondigitalocean.app/api/v1",
			Model:                 "gpt-4o-mini",
			Timeout:               60,
			SkipPersonaPrompt:     true, // DO agents have built-in persona
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"celeste-classic": {
			BaseURL:               "https://api.openai.com/v1",
			Model:                 "gpt-4o-mini",
			Timeout:               60,
			SkipPersonaPrompt:     false,
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClassic,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
		"celeste-claw": {
			BaseURL:               "https://api.openai.com/v1",
			Model:                 "gpt-4o-mini",
			Timeout:               60,
			SkipPersonaPrompt:     false,
			SimulateTyping:        true,
			TypingSpeed:           25,
			RuntimeMode:           config.RuntimeModeClaw,
			ClawMaxToolIterations: config.DefaultClawMaxToolIterations,
		},
	}

	tmpl, ok := templates[strings.ToLower(name)]
	if !ok {
		return fmt.Errorf("unknown config template '%s'. Available: openai, grok, elevenlabs, venice, digitalocean, celeste-classic, celeste-claw", name)
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
	fmt.Printf("\nEdit the file to add your API key, then run:\n")
	fmt.Printf("  celeste -config %s chat\n", name)
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
	cfg, err := config.Load()
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

	cfg, _ := config.Load()
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
	cfg, err := config.Load()
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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "No API key configured.")
		os.Exit(1)
	}

	// Initialize LLM client
	llmConfig := &llm.Config{
		APIKey:            cfg.APIKey,
		BaseURL:           cfg.BaseURL,
		Model:             cfg.Model,
		Timeout:           cfg.GetTimeout(),
		SkipPersonaPrompt: cfg.SkipPersonaPrompt,
		Collections:       cfg.Collections,
		XAIFeatures:       cfg.XAIFeatures,
	}
	client := llm.NewClient(llmConfig, nil)

	if !cfg.SkipPersonaPrompt {
		client.SetSystemPrompt(prompts.GetSystemPrompt(false))
	}

	// Send message
	ctx, cancel := context.WithTimeout(context.Background(), cfg.GetTimeout())
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
	cfg, err := config.Load()
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
	contextLimit := cfg.ContextLimit
	if contextLimit == 0 {
		contextLimit = config.GetModelLimit(cfg.Model)
	}
	contextTracker := config.NewContextTracker(session, cfg.Model, contextLimit)

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
	cfg, err := config.Load()
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
	contextLimit := cfg.ContextLimit
	if contextLimit == 0 {
		contextLimit = config.GetModelLimit(cfg.Model)
	}
	contextTracker := config.NewContextTracker(session, cfg.Model, contextLimit)

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
	cfg, err := config.Load()
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

	// Load config
	cfg, err := config.Load()
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

	cfg, err := config.LoadNamed(configName)
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

	srv := server.New(serverCfg)
	server.RegisterHandlers(srv)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Serve(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
