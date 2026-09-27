package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/subagents"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// chatDeps are the pieces runChatTUI wires to the Bubble Tea program after
// it exists (prompt/ask funcs need p.Send), and that tests drive directly.
type chatDeps struct {
	registry   *tools.Registry
	adapter    *TUIClientAdapter
	mcpManager *mcp.Manager
	indexer    *codegraph.Indexer
}

// newChatApp builds the chat TUI model exactly as runChatTUI did, up to (not
// including) tea.NewProgram. Errors that runChatTUI reported with os.Exit
// are returned instead.
func newChatApp(cfg *config.Config, cwd, homeDir string) (tui.AppModel, *chatDeps, error) {
	// Initialize file checkpointing for stale detection and undo support
	fileTracker := checkpoints.NewFileTracker()
	snapshotMgr := checkpoints.NewSnapshotManager(fmt.Sprintf("tui-%d", os.Getpid()))

	// Initialize tool registry
	registry := tools.NewRegistry()
	configLoader := newBuiltinConfigAdapter(config.NewConfigLoader(cfg))
	builtin.RegisterAll(registry, cwd, configLoader, fileTracker, snapshotMgr)
	builtin.RegisterCollectionsTools(registry, cfg)
	_ = registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills"))

	// Register subagent spawning tool — available in all modes so chat
	// users can delegate subtasks and parameterize subagent persona.
	isChild := os.Getenv("CELESTE_SUBAGENT") == "1"
	subMgr := subagents.NewManager(cfg, cwd, isChild)
	registry.RegisterWithModes(
		subagents.NewSpawnAgentTool(subMgr),
		tools.ModeAgent, tools.ModeClaw, tools.ModeChat,
	)
	// Register inter-agent mailbox messaging tool (#31). The top-level
	// orchestrator posts as "parent"; subagents receive a per-element
	// instance via their own tool registry (future work — see #31).
	registry.RegisterWithModes(
		subagents.NewPostMessageTool(subMgr, "parent"),
		tools.ModeAgent, tools.ModeClaw, tools.ModeChat,
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
	checker.SetConfigPath(permConfigPath)
	registry.SetPermissionChecker(checker)

	// Initialize MCP servers (external tool providers) with 5-second timeout.
	// Merge celeste-native, foreign (claude/cursor), and project-level configs;
	// the per-server `enabled` gate still decides what actually connects.
	mcpPaths := mcp.DiscoverConfigPaths(cwd, homeDir)
	mcpManager := mcp.NewManagerMulti(mcpPaths, registry)
	// Merged config drives the /mcp panel (shows configured-but-disconnected
	// servers too); ignore a load error here — Start below already reports it.
	mcpMerged, _ := mcp.LoadMerged(mcpPaths)
	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := mcpManager.Start(mcpCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP initialization failed: %v\n", err)
	}
	mcpCancel()

	// ponytail: discovery mode is dead weight until the tool list is actually
	// big. Flip it on past a threshold so small setups keep every tool visible.
	// Upgrade path: make the threshold (and an explicit on/off) a config field.
	const toolDiscoveryThreshold = 40
	if registry.Count() > toolDiscoveryThreshold {
		registry.SetDiscoveryMode(true)
	}

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
	// With the persona skipped, the prompt is just the project context (empty
	// when there is none).
	client.SetSystemPrompt(prompts.GetSystemPromptWithContext(cfg.SkipPersonaPrompt, projectContext, gitSnapshotContent))

	// Auto-scan collections if management key is set.
	// Also prunes stale collection IDs that no longer exist in the API.
	if cfg.XAIManagementAPIKey != "" {
		go func() {
			client := collections.NewClient(cfg.XAIManagementAPIKey)
			cols, err := client.ListCollections()
			if err != nil {
				return
			}

			// Prune stale active collection IDs
			if cfg.Collections != nil && len(cfg.Collections.ActiveCollections) > 0 {
				validIDs := make(map[string]bool)
				for _, col := range cols {
					validIDs[col.ID] = true
				}
				var kept []string
				pruned := 0
				for _, id := range cfg.Collections.ActiveCollections {
					if validIDs[id] {
						kept = append(kept, id)
					} else {
						pruned++
					}
				}
				if pruned > 0 {
					cfg.Collections.ActiveCollections = kept
					_ = config.Save(cfg)
					fmt.Fprintf(os.Stderr, "📚 Pruned %d stale collection IDs from config\n", pruned)
				}
			}

			activeCount := 0
			if cfg.Collections != nil {
				activeCount = len(cfg.Collections.ActiveCollections)
			}
			if len(cols) > 0 {
				fmt.Fprintf(os.Stderr, "📚 %d collections available (%d active) — /collections to manage\n", len(cols), activeCount)
			}
		}()
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
		client:         client,
		registry:       registry,
		baseConfig:     cfg,
		costTracker:    costs.NewSessionTracker(),
		subMgr:         subMgr,
		projectContext: projectContext,
		gitSnapshot:    gitSnapshotContent,
	}

	// Initialize logging for skill calls
	if err := tui.InitLogging(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to init logging: %v\n", err)
	}

	// Initialize session management
	sessionManager := config.NewSessionManager()
	var currentSession *config.Session

	// Start a fresh session for each chat invocation unless `celeste resume
	// <id>` asked for a saved one. Auto-resume was causing cross-contamination
	// between agent and chat sessions (agent markers like
	// STEP_DONE/TASK_COMPLETE leaked into chat).
	if resumeSessionID != "" {
		if s, err := sessionManager.Load(resumeSessionID); err == nil {
			fmt.Fprintf(os.Stderr, "📂 Resuming session %s (%d messages)\n", s.ID, len(s.Messages))
			currentSession = s
		} else {
			fmt.Fprintf(os.Stderr, "Could not load session %s: %v — starting a new one\n", resumeSessionID, err)
		}
	}
	if currentSession == nil {
		fmt.Fprintln(os.Stderr, "📝 Starting new session")
		currentSession = sessionManager.NewSession()
	}

	// Create TUI with session management
	app := tui.NewApp(tuiClient)

	// Set version information
	app = app.SetVersion(Version, Build)

	// Set configuration (for context limits, etc.)
	app = app.SetConfig(cfg)

	// Pass grimoire and code graph data to TUI for /grimoire and /index commands
	if grimoireContent != "" {
		app = app.WithGrimoireContent(grimoireContent)
	}
	if codeGraphSummary != "" {
		app = app.WithCodeGraphSummary(codeGraphSummary)
	}
	if indexer != nil {
		app = app.WithCodeGraphIndexer(indexer)
	}

	// Restore messages from session if available
	if len(currentSession.Messages) > 0 {
		app = app.WithMessages(tui.ChatMessagesFromSession(currentSession.Messages))
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

	// Inject working dir + permission checker for the segmented status line.
	app = app.SetWorkDir(cwd).SetPermissionChecker(checker)

	// Wire the MCP manager + discovered configs into the /mcp panel for runtime
	// connect/disconnect/toggle.
	var mcpConfigs map[string]mcp.ServerConfig
	if mcpMerged != nil {
		mcpConfigs = mcpMerged.Servers
	}
	app = app.SetMCPManager(mcpManager, mcpConfigs)

	return app, &chatDeps{registry: registry, adapter: tuiClient, mcpManager: mcpManager, indexer: indexer}, nil
}
