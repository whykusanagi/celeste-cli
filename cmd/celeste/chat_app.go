package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
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
	env      *loop.Env // owns MCP clients, the code graph and hooks; Close it last
	registry *tools.Registry
	adapter  *TUIClientAdapter
	hooks    *hooks.Runner

	// restoreMigrationWarn undoes newChatApp's routing of config.MigrationWarn
	// to the tui log (#144 W6b review, I1(b)). runChatTUI's caller doesn't
	// need to call it — the process is exiting — but tests that build several
	// chat apps in one process do, via cleanupChatDeps.
	restoreMigrationWarn func()
}

// newChatApp builds the chat TUI model up to (not including) tea.NewProgram.
// loop.Setup(ModeChat) builds the registry, permissions, hooks, MCP clients,
// grimoire, memories, git state and code graph (2.0 F2d); the TUI-only tools
// go on top. The caller owns closing the deps (cleanupChatDeps in tests,
// runChatTUI's defers).
func newChatApp(cfg *config.Config, cwd, homeDir string) (tui.AppModel, *chatDeps, error) {
	// First, so Setup's warnings reach the log.
	if err := tui.InitLogging(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to init logging: %v\n", err)
	}

	// From here on, a migration note (config/migrate.go's MigrationWarn) must
	// reach the log instead of stderr: the alt screen is about to take over
	// the terminal, and /endpoint or SwitchEndpoint can still load a
	// different, not-yet-migrated profile while it's up (#144 W6b review,
	// I1(b)). The chat startup path itself migrates every profile once,
	// before this point, via MigrateConfigDir in runChatTUI — notes from
	// that one call are still fine on stderr.
	prevMigrationWarn := config.MigrationWarn
	config.MigrationWarn = func(msg string) { tui.LogInfo("celeste: " + msg) }
	restoreMigrationWarn := func() { config.MigrationWarn = prevMigrationWarn }

	// Use the model the provider serves now: a retired one is replaced for
	// this process. The resolved models live on a copy: cfg itself reaches
	// paths that save it (collections, /voice), and the config file must
	// keep what the user wrote. This may fetch the provider's catalog
	// (bounded by its timeout); we're not in the TUI yet.
	served := *cfg
	modelNotes := served.ResolveServedModels(context.Background())
	for _, n := range modelNotes {
		fmt.Fprintln(os.Stderr, "⚠ "+n)
	}

	// The session comes before Setup: its ID is the hooks' session_id.
	// A fresh session per chat unless `celeste resume <id>` asked for one
	// (auto-resume leaked agent markers into chat).
	sessionManager := config.NewSessionManager()
	var currentSession *config.Session
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
	resumed := resumeSessionID != "" && len(currentSession.Messages) > 0

	autoInitGrimoire(cwd)

	sink := newChatWarnSink()
	env, err := loop.Setup(loop.ModeChat, &served, cwd, loop.SetupOptions{
		SessionID: currentSession.ID,
		Warn:      sink.warn,
		Notice:    sink.warn,
		Approve:   chatHookApprover,
	})
	if err != nil {
		restoreMigrationWarn()
		return tui.AppModel{}, nil, err
	}
	registry := env.Registry

	// TUI-only tools (F2a: Setup never registers them).
	registerChatOnlyTools(registry, cfg)
	// Subagents: chat users can delegate subtasks and parameterize their
	// persona. The top-level chat posts to the mailbox as "parent" (#31).
	isChild := os.Getenv("CELESTE_SUBAGENT") == "1"
	subMgr := subagents.NewManager(&served, cwd, isChild)
	registry.RegisterWithModes(subagents.NewSpawnAgentTool(subMgr), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(subagents.NewPostMessageTool(subMgr, "parent"), tools.ModeAgent, tools.ModeChat)
	env.RefreshDiscovery()

	client := llm.NewClient(&llm.Config{
		APIKey:            cfg.APIKey,
		BaseURL:           cfg.BaseURL,
		Model:             served.Model,
		Timeout:           cfg.GetTimeout(),
		SkipPersonaPrompt: cfg.SkipPersonaPrompt,
		SimulateTyping:    cfg.SimulateTyping,
		TypingSpeed:       cfg.TypingSpeed,
		Collections:       cfg.Collections,
		XAIFeatures:       cfg.XAIFeatures,
	}, registry)
	source := "startup"
	if resumed {
		source = "resume"
	}
	env.StartSession(context.Background(), source)
	client.SetSystemPrompt(env.SystemPrompt("", nil))

	scanCollections(cfg)

	tuiClient := &TUIClientAdapter{
		client:         client,
		registry:       registry,
		baseConfig:     &served,
		costTracker:    costs.NewSessionTracker(),
		subMgr:         subMgr,
		projectContext: env.ProjectContext,
		gitSnapshot:    env.GitSnapshot,
		hooks:          env.Hooks,
		rules:          env.Rules,
	}
	tuiClient.lifeCtx, tuiClient.lifeCancel = context.WithCancel(context.Background())
	tuiClient.gate = chatGate(registry)

	// Subagents and /agent nest under the chat's Env (2.0 F2e): they share
	// its MCP clients (global servers only), hooks (this session's ID) and
	// code graph instead of starting their own, and their warnings reach
	// the chat.
	subMgr.UseParent(env, tuiAgentWarn)
	tuiClient.parentEnv = env

	app := tui.NewApp(tuiClient)
	app = app.SetVersion(Version, Build)
	app = app.SetConfig(cfg)
	if env.GrimoireContext != "" {
		app = app.WithGrimoireContent(env.GrimoireContext)
	}
	if env.CodeGraphSummary != "" {
		app = app.WithCodeGraphSummary(env.CodeGraphSummary)
	}
	if env.Indexer != nil {
		app = app.WithCodeGraphIndexer(env.Indexer)
	}
	if len(currentSession.Messages) > 0 {
		app = app.WithMessages(tui.ChatMessagesFromSession(currentSession.Messages))
	}
	// Setup's warnings printed before the alt screen hid stderr; show them
	// in the chat too.
	for _, w := range sink.done() {
		app = app.WithSystemMessage("⚠ " + w)
	}
	for _, n := range modelNotes {
		app = app.WithSystemMessage("⚠ " + n)
	}

	app = restoreEndpoint(app, cfg, tuiClient, sessionManager, currentSession)

	if hist := currentSession.GetCommandHistory(); len(hist) > 0 {
		app = app.WithCommandHistory(hist)
	}
	if currentSession.GetModel() == "" {
		tui.LogInfo(fmt.Sprintf("Setting model from config: %s", served.Model))
		currentSession.SetModel(served.Model)
		if err := sessionManager.Save(currentSession); err != nil {
			log.Printf("Warning: Failed to save session with model: %v", err)
		}
	}
	app = app.SetSessionManager(&SessionManagerAdapter{manager: sessionManager}, currentSession)
	app = app.SetWorkDir(cwd).SetPermissionChecker(env.Checker)

	// The /mcp panel shows configured-but-disconnected servers too; a load
	// error was already reported by Setup.
	var mcpConfigs map[string]mcp.ServerConfig
	if merged, _ := mcp.LoadMerged(mcp.DiscoverConfigPaths(cwd, homeDir)); merged != nil {
		mcpConfigs = merged.Servers
	}
	app = app.SetMCPManager(env.MCP, mcpConfigs)

	return app, &chatDeps{env: env, registry: registry, adapter: tuiClient, hooks: env.Hooks, restoreMigrationWarn: restoreMigrationWarn}, nil
}

// autoInitGrimoire creates a .grimoire (and the project's first-visit
// memory) when the workspace has none, before loop.Setup loads it. A load
// error also reaches the init, as before; Setup reports the error itself.
func autoInitGrimoire(cwd string) {
	if g, err := grimoire.LoadAll(cwd); err == nil && g != nil && !g.IsEmpty() {
		return
	}
	fmt.Fprintf(os.Stderr, "📖 No .grimoire found — creating one...\n")
	if _, err := grimoire.Init(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-init grimoire failed: %v\n", err)
	} else {
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

// scanCollections lists the xAI collections in the background when a
// management key is set, pruning stale active collection IDs from config.
func scanCollections(cfg *config.Config) {
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
}

// restoreEndpoint restores the endpoint/provider from the session, or
// detects it from the config's base URL.
func restoreEndpoint(app tui.AppModel, cfg *config.Config, a *TUIClientAdapter, sm *config.SessionManager, s *config.Session) tui.AppModel {
	// Restore endpoint/provider from session, or detect from config
	sessionEndpoint := s.GetEndpoint()
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
			// Its agent and small models drive /agent, /orchestrate and
			// the summarizer: resolve them (we're not in the TUI yet).
			if !namedCfg.ModelPinned() {
				providers.PrepareModels(context.Background(), providers.DetectProvider(namedCfg.BaseURL), namedCfg.BaseURL, namedCfg.APIKey, namedCfg.AgentModel, namedCfg.SmallModel)
			}
			a.baseConfig = servedAgentModels(namedCfg)
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
			s.SetEndpoint(detectedProvider)
			// Save the session with the detected endpoint
			if err := sm.Save(s); err != nil {
				log.Printf("Warning: Failed to save session with detected endpoint: %v", err)
			} else {
				tui.LogInfo(fmt.Sprintf("✓ Saved session with endpoint: %s", detectedProvider))
			}
		} else {
			tui.LogInfo("⚠ Could not detect provider from BaseURL")
		}
	}
	return app
}

// registerChatOnlyTools adds the config-backed skills and collections search
// to Setup's registry. Before F2d they were registered ahead of the user's
// custom skills (~/.celeste/skills), so a custom skill with the same name
// won; keep that. At this point only a custom skill can hold one of these
// names (builtins never do, MCP tools are mcp__-prefixed), and custom skills
// are registered for all modes, so putting back what was replaced restores
// the old order exactly.
func registerChatOnlyTools(registry *tools.Registry, cfg *config.Config) {
	before := map[string]tools.Tool{}
	for _, t := range registry.GetAll() {
		before[t.Name()] = t
	}
	builtin.RegisterConfigTools(registry, newBuiltinConfigAdapter(config.NewConfigLoader(cfg)))
	builtin.RegisterCollectionsTools(registry, cfg)
	for name, t := range before {
		if cur, _ := registry.Get(name); cur != t {
			registry.Register(t)
		}
	}
}
