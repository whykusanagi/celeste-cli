package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// servedConfig returns a copy of cfg on the models its provider serves now
// (providers.ResolveModel), so a retired configured model is never sent. The
// catalog comes from the cache (refreshed in the background once stale), or
// one bounded fetch the first time. Each note is logged once per process.
func (s *Server) servedConfig(ctx context.Context, cfg *config.Config) *config.Config {
	c := *cfg
	for _, n := range c.ResolveServedModels(ctx) {
		if _, seen := s.modelNotes.LoadOrStore(n, true); !seen {
			log.Printf("[mcp-server] %s", n)
		}
	}
	return &c
}

// validateWorkspace ensures the workspace path is safe.
// Rejects paths outside the server's original workspace or the user's home directory.
func validateWorkspace(requested, serverWorkspace string) error {
	if requested == "" || requested == serverWorkspace {
		return nil
	}

	// Resolve to absolute path
	absRequested, err := filepath.Abs(requested)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Must be under user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory")
	}

	if !strings.HasPrefix(absRequested, homeDir+"/") {
		return fmt.Errorf("workspace must be under home directory (%s)", homeDir)
	}

	// Reject sensitive directories
	sensitive := []string{".ssh", ".gnupg", ".aws", ".config/gcloud", ".kube"}
	for _, dir := range sensitive {
		if strings.Contains(absRequested, "/"+dir) {
			return fmt.Errorf("access to %s is not allowed", dir)
		}
	}

	return nil
}

// RegisterHandlers registers all MCP tool handlers on the server.
// Persona tools (celeste / celeste_content / celeste_status) route
// through a chat LLM and are kept for the "ask Celeste a question" use
// case. Direct codegraph tools (celeste_index + celeste_code_* family)
// skip the LLM and serve queries straight from the cached graph — use
// those for tool-driven workflows that need verbatim results.
func RegisterHandlers(s *Server) {
	registerCelesteTool(s)
	registerCelesteContentTool(s)
	registerCelesteStatusTool(s)
	registerCodegraphTools(s)
}

// removeIfExists deletes a file at path if present, swallowing
// not-found errors. Used by indexRebuild to clear stale SQLite files
// before re-opening.
func removeIfExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}

// --- celeste tool ---

func celesteToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "What you want Celeste to do"
			},
			"mode": {
				"type": "string",
				"enum": ["chat", "agent"],
				"default": "chat",
				"description": "Execution mode: chat for single turn, agent for multi-step autonomous work"
			},
			"workspace": {
				"type": "string",
				"description": "Working directory (defaults to server cwd)"
			}
		},
		"required": ["prompt"]
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste",
		Description: "Delegate a task to Celeste, an agentic AI assistant with her own persona and development capabilities.",
		InputSchema: schema,
	}
}

func registerCelesteTool(s *Server) {
	s.RegisterTool(celesteToolDef(), func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("prompt is required")
		}

		mode, _ := args["mode"].(string)
		if mode == "" {
			mode = "chat"
		}

		workspace, _ := args["workspace"].(string)
		if workspace == "" {
			workspace = s.config.Workspace
		}

		// Security: validate workspace is a safe directory
		// Reject absolute paths outside of user's home to prevent directory traversal
		if err := validateWorkspace(workspace, s.config.Workspace); err != nil {
			return nil, fmt.Errorf("workspace rejected: %w", err)
		}

		cfg := s.config.CelesteConfig
		if cfg == nil {
			return nil, fmt.Errorf("celeste config not loaded")
		}

		switch mode {
		case "agent":
			return s.runAgentMode(ctx, cfg, prompt, workspace)
		default:
			return s.runChatMode(ctx, cfg, prompt, workspace)
		}
	})
}

// backgroundAfter is how long an MCP agent run may hold the call open before it
// is handed back as a handle. 60s from measurement, not taste: plain fugu
// finishes a substantial prompt in ~45s and so returns inline, while fugu-ultra
// takes 211-300s and always crosses it. Package var so tests can shorten it.
var backgroundAfter = 60 * time.Second

// agentOutcome is the result of one agent run, flattened so the execution can be
// swapped for a fake in tests without constructing a real Runner.
type agentOutcome struct {
	Text       string
	Turns      int
	ToolCalls  int
	AgentRunID string
}

// agentExecFn runs the agent to completion. Indirected so tests can substitute a
// fake slow run and make the threshold deterministic.
var agentExecFn = execAgent

// execAgent runs a multi-turn agent loop for complex tasks.
func execAgent(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
	var outBuf, errBuf bytes.Buffer
	var warnMu sync.Mutex
	var warnings []string

	opts := agent.Options{
		// Setup and hook warnings go back to the MCP caller with the result;
		// there is no terminal to print them to.
		Warn: func(s string) {
			warnMu.Lock()
			defer warnMu.Unlock()
			warnings = append(warnings, s)
		},
		Workspace: workspace,
		MaxTurns:  50,
		// Route MCP agent-mode work to the agent model (task e8775b91).
		Model: cfg.ResolveAgentModel(),
		// MCP `celeste agent` mode is headless (no approval modal), like a
		// subagent. Without this, every write/exec tool resolves to "Ask" and is
		// denied, so the MCP-driven agent can't bash/write/commit. Invoking the
		// MCP agent tool IS the approval (task a035f219).
		AutoApproveTools: true,
		Verbose:          false,
		// The prompt is the caller's goal: UserPromptSubmit sees it once,
		// before any model call (2.0 F2e).
		CheckGoal: true,
	}

	if tally := costFrom(ctx); tally != nil {
		model := opts.Model
		opts.OnTurnStats = func(st agent.TurnStats) {
			if st.InputTokens+st.OutputTokens > 0 {
				tally.record(model, &llm.TokenUsage{PromptTokens: st.InputTokens, CompletionTokens: st.OutputTokens})
			}
		}
	}

	runner, err := agent.NewRunner(cfg, opts, &outBuf, &errBuf)
	if err != nil {
		return agentOutcome{}, fmt.Errorf("create agent runner: %w", err)
	}
	// execAgent owns the whole run, so the runner is closed only when the run
	// truly ends — including in the background case, where this function is
	// executing inside the watcher goroutine. Closing it in runAgentMode instead
	// would release the codegraph SQLite handle while a background run was still
	// using it.
	defer runner.Close()

	state, err := runner.RunGoal(ctx, goal)
	healthFrom(ctx).record(err) // nil-safe: a direct call in tests has no tally
	if err != nil {
		return agentOutcome{}, fmt.Errorf("agent error: %w", err)
	}

	// Build response with tool call history for transparency
	var sb strings.Builder

	// Tool call summary from steps
	toolSteps := 0
	for _, step := range state.Steps {
		if step.Type == "tool_call" || step.Name != "" {
			toolSteps++
		}
	}
	if toolSteps > 0 {
		sb.WriteString("## Tool Calls\n\n")
		for _, step := range state.Steps {
			if step.Name == "" {
				continue
			}
			preview := step.Content
			if len(preview) > 200 {
				preview = preview[:197] + "..."
			}
			sb.WriteString(fmt.Sprintf("- **%s** → %s\n", step.Name, preview))
		}
		sb.WriteString("\n")
	}

	// Agent response. Strip a fabricated "subagent spawned (id: …)" claim if
	// spawn_agent never actually ran this turn (task 04d48b1e — a weak model
	// flails then hallucinates a spawn).
	spawnRan := false
	for _, step := range state.Steps {
		if step.Name == "spawn_agent" {
			spawnRan = true
			break
		}
	}
	response := state.LastAssistantResponse
	if response == "" && outBuf.Len() > 0 {
		response = outBuf.String()
	}
	response = rules.StripUnbackedSpawnClaim(response, spawnRan)
	if response != "" {
		sb.WriteString("## Response\n\n")
		sb.WriteString(response)
	}

	warnMu.Lock()
	sb.WriteString(formatWarnings(warnings))
	warnMu.Unlock()

	// Metadata
	sb.WriteString(fmt.Sprintf("\n\n---\n_Agent: %d turns, status: %s_\n", state.Turn, state.Status))

	result := sb.String()
	if result == "" {
		result = fmt.Sprintf("Agent completed (%s) after %d turns", state.Status, state.Turn)
	}

	toolCalls := 0
	for _, step := range state.Steps {
		if step.Name != "" {
			toolCalls++
		}
	}
	return agentOutcome{
		Text:       result,
		Turns:      state.Turn,
		ToolCalls:  toolCalls,
		AgentRunID: state.RunID,
	}, nil
}

// formatWarnings renders the run's setup and hook warnings as a section for
// the tool result, or "" when there are none.
func formatWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n## Warnings\n\n")
	for _, w := range warnings {
		sb.WriteString("- " + w + "\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// runAgentMode runs a multi-turn agent loop, handing the caller a handle if the
// run outlives backgroundAfter. Mirrors subagents.Manager's BackgroundAfter
// mechanism: race the execution against a timer, return inline if it wins.
func (s *Server) runAgentMode(ctx context.Context, cfg *config.Config, goal, workspace string) ([]ContentBlock, error) {
	cfg = s.servedConfig(ctx, cfg)
	// The background goroutine must outlive this request, so it cannot inherit
	// the request context — that is cancelled the moment we return the handle.
	ctx = withHealth(withCost(ctx, &s.cost), &s.health)
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	type outcome struct {
		res agentOutcome
		err error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		// A panic here (in a background run, agentExecFn's own goroutine) would
		// otherwise be an unrecovered panic on a goroutine the runtime has no
		// other handler for, which kills the whole process — every other
		// in-flight run and the client's entire MCP session, not just this one
		// call. Recover and report it as this run's failure instead. The defer
		// must send on resultCh exactly once: either this recover fires (the
		// happy-path send below never ran, because the panic unwound past it),
		// or the happy path sent already and there was nothing to recover.
		defer func() {
			if r := recover(); r != nil {
				resultCh <- outcome{agentOutcome{}, fmt.Errorf("agent run panicked: %v", r)}
			}
		}()
		res, err := agentExecFn(runCtx, cfg, goal, workspace)
		resultCh <- outcome{res, err}
	}()

	select {
	case out := <-resultCh:
		// Finished inline. Nothing registered, response shape unchanged.
		cancel()
		if out.err != nil {
			return nil, out.err
		}
		return []ContentBlock{{Type: "text", Text: out.res.Text}}, nil

	case <-time.After(backgroundAfter):
		id := newRunID(time.Now())
		s.registerRun(id, cancel)

		go func() {
			out := <-resultCh
			final := &BackgroundRun{
				Status:     "completed",
				Result:     out.res.Text,
				Turns:      out.res.Turns,
				ToolCalls:  out.res.ToolCalls,
				AgentRunID: out.res.AgentRunID,
			}
			if out.err != nil {
				final.Status = "failed"
				final.Error = out.err.Error()
			}
			s.completeRun(id, final)
			cancel()
		}()

		handle, _ := json.Marshal(map[string]any{
			"run_id": id,
			"status": "running",
			"poll":   fmt.Sprintf("celeste_status with run_id=%s", id),
		})
		return []ContentBlock{{Type: "text", Text: string(handle)}}, nil
	}
}

// --- celeste_content tool ---

func celesteContentToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "What content to generate"
			},
			"format": {
				"type": "string",
				"enum": ["markdown", "plain", "html"],
				"default": "markdown",
				"description": "Output format for the generated content"
			}
		},
		"required": ["prompt"]
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_content",
		Description: "Generate content with Celeste's persona and voice. Blog posts, docs, commit messages, social posts, READMEs.",
		InputSchema: schema,
	}
}

func registerCelesteContentTool(s *Server) {
	s.RegisterTool(celesteContentToolDef(), func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("prompt is required")
		}

		format, _ := args["format"].(string)
		if format == "" {
			format = "markdown"
		}

		cfg := s.config.CelesteConfig
		if cfg == nil {
			return nil, fmt.Errorf("celeste config not loaded")
		}
		cfg = s.servedConfig(ctx, cfg)

		registry := tools.NewRegistry()
		client := llm.NewClient(llm.ConfigFrom(cfg), registry)

		// Use the content-specific prompt variant. It steps down like chat
		// on a small window; MCP responses never carry the guard's notice
		// (W5 ruling 7: frozen shape), it is only logged.
		window, _ := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit, cfg.APIKey)
		// The persona (Static) and the rest go to the client apart, so an
		// Anthropic request caches the persona on its own (#309).
		contentPrompt := prompts.ContentPrompt(window, "", format, "", "")
		contentPrompt.Dynamic += fmt.Sprintf("\n\nOutput format: %s\n", format)

		// Inject workspace grimoire if available (project-specific rules/context)
		cwd, _ := os.Getwd()
		if cwd != "" {
			if projectGrimoire, err := grimoire.LoadAll(cwd); err == nil && projectGrimoire != nil && !projectGrimoire.IsEmpty() {
				contentPrompt.Dynamic += "\n\n# Project Context (.grimoire)\n\n" + projectGrimoire.Render()
			}
		}

		client.SetSystemPromptParts(contentPrompt.Static, contentPrompt.Dynamic)

		messages := []tui.ChatMessage{
			{Role: "user", Content: prompt, Timestamp: time.Now()},
		}

		result, err := client.SendMessageSync(ctx, messages, nil)
		s.health.record(err)
		if err != nil {
			return nil, fmt.Errorf("content generation error: %w", err)
		}
		s.cost.record(cfg.Model, result.Usage)

		return []ContentBlock{{Type: "text", Text: strings.TrimSpace(result.Content)}}, nil
	})
}

// --- celeste_status tool ---

func celesteStatusToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"run_id": {
				"type": "string",
				"description": "Report on a background agent run instead of the server. Use the run_id returned when an agent run exceeded the inline threshold."
			},
			"cancel": {
				"type": "boolean",
				"description": "With run_id, cancel that run instead of reporting on it."
			}
		}
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_status",
		Description: "Get Celeste's current status: connected providers, loaded grimoire, indexed project, session cost. With run_id, report on (or cancel) a background agent run instead.",
		InputSchema: schema,
	}
}

func registerCelesteStatusTool(s *Server) {
	startTime := time.Now()

	s.RegisterTool(celesteStatusToolDef(), func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		if rawID, ok := args["run_id"].(string); ok && rawID != "" {
			if doCancel, _ := args["cancel"].(bool); doCancel {
				s.cancelRun(rawID)
			}
			run, found := s.lookupRun(rawID)
			if !found {
				return []ContentBlock{{Type: "text", Text: fmt.Sprintf(
					"unknown run %q. Background runs are held in memory by this MCP server, so a client restart ends them — the run is gone rather than lost. Checkpoints remain on disk; `celeste agent -list-runs` shows them.",
					rawID)}}, nil
			}

			out := map[string]any{
				"run_id":     run.ID,
				"status":     run.Status,
				"turns":      run.Turns,
				"tool_calls": run.ToolCalls,
			}
			if run.Status == "running" {
				out["elapsed"] = time.Since(run.StartedAt).Round(time.Second).String()
			} else {
				out["elapsed"] = run.EndedAt.Sub(run.StartedAt).Round(time.Second).String()
			}
			if run.Result != "" {
				out["result"] = run.Result
			}
			if run.Error != "" {
				out["error"] = run.Error
			}
			if run.AgentRunID != "" {
				out["agent_run_id"] = run.AgentRunID
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return []ContentBlock{{Type: "text", Text: string(b)}}, nil
		}

		cfg := s.config.CelesteConfig

		status := map[string]any{
			"server":  serverName,
			"version": serverVersion,
			// The commit is what makes a stale server visible: version alone is a
			// release-please constant and reads the same for a shipped release and
			// a local build several merges ahead. An MCP process left running
			// across a reinstall keeps serving the old binary until the client
			// restarts, and this is the field that says so.
			"commit": BuildCommit(),
			"uptime": time.Since(startTime).Round(time.Second).String(),
			// "degraded" while the latest completion failed (2.0 W3).
			"health": s.health.state(),
		}

		if cfg != nil {
			status["provider"] = providers.CleanBaseURL(cfg.BaseURL)
			status["model"] = cfg.Model
		}

		status["workspace"] = s.config.Workspace
		status["transport"] = s.config.Transport
		// Additive fields (#210, spec §3.1): the plugin's celeste-context
		// skill reads them.
		status["grimoire"] = grimoireStatus(s.config.Workspace)
		status["project"] = s.projectStatus(s.config.Workspace)
		status["session_cost"] = s.cost.snapshot()
		// Additive fields (2.0 W3): completion outcomes, the oracle's
		// latency and hit rate, and stream-rule fires.
		status["completions"] = s.health.snapshot()
		status["oracle"] = oracleStatus(cfg)
		status["rules"] = rulesStatus(cfg)

		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal status: %w", err)
		}

		return []ContentBlock{{Type: "text", Text: string(data)}}, nil
	})
}

// grimoireStatus reports whether a grimoire applies to workspace and which
// files it came from: the workspace's .grimoire, fragments, parent
// directories and ~/.celeste/grimoire.md (grimoire.Discover's order).
func grimoireStatus(workspace string) map[string]any {
	out := map[string]any{"loaded": false, "sources": []string{}}
	if workspace == "" {
		return out
	}
	g, err := grimoire.LoadAll(workspace)
	if err != nil || g == nil || g.IsEmpty() {
		return out
	}
	out["loaded"] = true
	if len(g.Sources) > 0 {
		out["sources"] = g.Sources
	}
	return out
}

// projectStatus reports whether the workspace has a built code graph and its
// size. It opens an index only when one exists on disk (or is cached), so a
// status call never builds an index or creates its directory.
func (s *Server) projectStatus(workspace string) map[string]any {
	out := map[string]any{"indexed": false}
	if workspace == "" {
		return out
	}
	s.indexerMu.Lock()
	idx := s.indexers[workspace]
	s.indexerMu.Unlock()
	if idx == nil {
		if _, err := os.Stat(codegraph.IndexPath(workspace)); err != nil {
			return out
		}
		var err error
		if idx, _, err = s.indexerFor(workspace); err != nil {
			out["error"] = err.Error()
			return out
		}
	}
	stats, err := idx.Stats()
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	if stats.TotalFiles == 0 {
		return out
	}
	out["indexed"] = true
	out["total_files"] = stats.TotalFiles
	out["total_symbols"] = stats.TotalSymbols
	out["total_edges"] = stats.TotalEdges
	return out
}
