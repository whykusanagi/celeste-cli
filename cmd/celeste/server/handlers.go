package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

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
			return runAgentMode(ctx, cfg, prompt, workspace)
		default:
			return runChatMode(ctx, cfg, prompt, workspace)
		}
	})
}

// toolErrorJSON builds a properly-escaped JSON tool-error payload.
func toolErrorJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"error": true, "message": msg})
	return string(b)
}

// toolCallBatchSig returns a signature of a tool-call batch INCLUDING arguments,
// so the repetition guard only trips on the model re-issuing the IDENTICAL call
// (a true stuck loop), never on legitimate bulk work where each call has distinct
// args (e.g. 30 different mp3 lines).
func toolCallBatchSig(calls []llm.ToolCallResult) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, c.Name+"("+c.Arguments+")")
	}
	return strings.Join(parts, ",")
}

// maxNoProgressStreak is how many turns of byte-identical tool RESULTS (same
// tool name producing the same output) trip the progress guard. Higher than the
// args-based maxSameCallStreak because args may legitimately vary; what signals
// a true stuck loop is the model getting the same result over and over.
const maxNoProgressStreak = 6

// progressGuard catches a stuck loop the args-based guard misses (task 8f02ed3d):
// the model re-calls the same tool with slightly-varying args but gets identical
// results turn after turn. It keys on the RESULT, not the args, so legitimate
// bulk work — where each call produces a distinct result (e.g. a new mp3 file) —
// never trips it.
type progressGuard struct {
	lastSig string
	streak  int
}

// observe records a turn's tool-result signature and reports whether the loop has
// produced identical results maxNoProgressStreak times in a row. An empty
// signature (no tool calls this turn) resets the streak.
func (g *progressGuard) observe(sig string) bool {
	if sig == "" {
		g.streak = 0
		g.lastSig = ""
		return false
	}
	if sig == g.lastSig {
		g.streak++
	} else {
		g.streak = 1
		g.lastSig = sig
	}
	return g.streak >= maxNoProgressStreak
}

// runChatMode executes a single-turn chat with Celeste's persona.
func runChatMode(ctx context.Context, cfg *config.Config, prompt, workspace string) ([]ContentBlock, error) {
	// Auto-init grimoire if not present
	if _, err := os.Stat(filepath.Join(workspace, ".grimoire")); os.IsNotExist(err) {
		_, _ = grimoire.Init(workspace)
	}

	registry := tools.NewRegistry()
	builtin.RegisterAll(registry, workspace, nil, nil, nil)

	llmConfig := &llm.Config{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: cfg.GetTimeout(),
	}
	client := llm.NewClient(llmConfig, registry)

	systemPrompt := prompts.GetSystemPrompt(cfg.SkipPersonaPrompt)

	// Load grimoire into system prompt for project context
	if projectGrimoire, err := grimoire.LoadAll(workspace); err == nil && projectGrimoire != nil && !projectGrimoire.IsEmpty() {
		systemPrompt += "\n\n# Project Context (.grimoire)\n\n" + projectGrimoire.Render()
	}

	client.SetSystemPrompt(systemPrompt)

	// Build tool definitions from registry so chat mode can call tools
	registeredTools := registry.GetTools(tools.ModeChat)
	var toolDefs []tui.SkillDefinition
	for _, t := range registeredTools {
		var params map[string]any
		if raw := t.Parameters(); raw != nil {
			_ = json.Unmarshal(raw, &params)
		}
		toolDefs = append(toolDefs, tui.SkillDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  params,
		})
	}

	messages := []tui.ChatMessage{
		{Role: "user", Content: prompt, Timestamp: time.Now()},
	}

	// ttsRan tracks whether generate_speech executed successfully this session.
	// The flag is session-scoped (persists across turns) and is used to detect
	// hallucinated "Audio saved:" prose when TTS never ran.
	ttsRan := false

	// Auto-loop: send message, execute tool calls, send results back, repeat
	maxLoops := 25
	const maxSameCallStreak = 3 // stop a true stuck loop: the IDENTICAL call repeated (mirrors the TUI guard)
	lastToolSig := ""
	sameToolStreak := 0
	var progress progressGuard // result-based guard for args-varying loops (task 8f02ed3d)
	for i := 0; i < maxLoops; i++ {
		result, err := client.SendMessageSync(ctx, messages, toolDefs)
		if err != nil {
			return nil, fmt.Errorf("chat error: %w", err)
		}

		// If no tool calls, we're done
		if len(result.ToolCalls) == 0 {
			text := llm.StripUnbackedAudioClaim(strings.TrimSpace(result.Content), ttsRan)
			return []ContentBlock{{Type: "text", Text: text}}, nil
		}

		// Repetition guard: stop only when the model re-issues the IDENTICAL call
		// (same tool AND args) several turns in a row — a true stuck loop. The
		// signature includes args, so legitimate bulk work (distinct lines) is
		// never blocked.
		if sig := toolCallBatchSig(result.ToolCalls); sig != "" {
			if sig == lastToolSig {
				sameToolStreak++
			} else {
				sameToolStreak = 1
				lastToolSig = sig
			}
			if sameToolStreak >= maxSameCallStreak {
				return []ContentBlock{{Type: "text", Text: fmt.Sprintf("Stopped: the model made the identical tool call %d times in a row (stuck loop).", sameToolStreak)}}, nil
			}
		} else {
			sameToolStreak = 0
			lastToolSig = ""
		}

		// Add assistant response with tool calls
		messages = append(messages, tui.ChatMessage{
			Role:    "assistant",
			Content: result.Content,
			ToolCalls: func() []tui.ToolCallInfo {
				var calls []tui.ToolCallInfo
				for _, tc := range result.ToolCalls {
					calls = append(calls, tui.ToolCallInfo{
						ID:        tc.ID,
						Name:      tc.Name,
						Arguments: tc.Arguments,
					})
				}
				return calls
			}(),
		})

		// Execute each tool call and add results. turnResults captures the
		// (tool name | result) of each call so the progress guard can detect a
		// loop that produces identical results despite varying args.
		var turnResults []string
		for _, tc := range result.ToolCalls {
			// Corruption detected upstream (dropped stream delta): never run the
			// tool with empty args — surface an error so the model can retry.
			if tc.ArgsError != "" {
				content := toolErrorJSON(tc.ArgsError)
				messages = append(messages, tui.ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    content,
				})
				turnResults = append(turnResults, tc.Name+"|"+content)
				continue
			}

			var args map[string]any
			if tc.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
					content := toolErrorJSON(fmt.Sprintf("invalid tool arguments JSON: %v", err))
					messages = append(messages, tui.ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Name,
						Content:    content,
					})
					turnResults = append(turnResults, tc.Name+"|"+content)
					continue
				}
			}
			if args == nil {
				args = make(map[string]any)
			}

			toolResult, err := registry.Execute(ctx, tc.Name, args)
			if err != nil {
				content := toolErrorJSON(fmt.Sprintf("tool execution failed: %v", err))
				messages = append(messages, tui.ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    content,
				})
				turnResults = append(turnResults, tc.Name+"|"+content)
				continue
			}
			if tc.Name == "generate_speech" && !toolResult.Error {
				ttsRan = true
			}
			messages = append(messages, tui.ChatMessage{
				Role:       "tool",
				Content:    toolResult.Content,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
			turnResults = append(turnResults, tc.Name+"|"+toolResult.Content)
		}

		// Progress guard: stop if the same tool(s) return identical results turn
		// after turn (a stuck loop the args-based guard misses). Distinct results
		// (e.g. bulk TTS writing a new file each call) reset the streak.
		if progress.observe(strings.Join(turnResults, ",")) {
			return []ContentBlock{{Type: "text", Text: fmt.Sprintf("Stopped: the model called the same tool with no new result %d turns in a row (stuck loop).", maxNoProgressStreak)}}, nil
		}
	}

	return []ContentBlock{{Type: "text", Text: "Tool loop limit reached"}}, nil
}

// runAgentMode runs a multi-turn agent loop for complex tasks.
func runAgentMode(ctx context.Context, cfg *config.Config, goal, workspace string) ([]ContentBlock, error) {
	// Auto-init grimoire if not present
	if _, err := os.Stat(filepath.Join(workspace, ".grimoire")); os.IsNotExist(err) {
		_, _ = grimoire.Init(workspace)
	}

	var outBuf, errBuf bytes.Buffer

	opts := agent.Options{
		Workspace: workspace,
		MaxTurns:  50,
		Verbose:   false,
	}

	runner, err := agent.NewRunner(cfg, opts, &outBuf, &errBuf)
	if err != nil {
		return nil, fmt.Errorf("create agent runner: %w", err)
	}
	defer runner.Close()

	state, err := runner.RunGoal(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("agent error: %w", err)
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

	// Agent response
	response := state.LastAssistantResponse
	if response == "" && outBuf.Len() > 0 {
		response = outBuf.String()
	}
	if response != "" {
		sb.WriteString("## Response\n\n")
		sb.WriteString(response)
	}

	// Metadata
	sb.WriteString(fmt.Sprintf("\n\n---\n_Agent: %d turns, status: %s_\n", state.Turn, state.Status))

	result := sb.String()
	if result == "" {
		result = fmt.Sprintf("Agent completed (%s) after %d turns", state.Status, state.Turn)
	}

	return []ContentBlock{{Type: "text", Text: result}}, nil
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

		registry := tools.NewRegistry()
		llmConfig := &llm.Config{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			Timeout: cfg.GetTimeout(),
		}
		client := llm.NewClient(llmConfig, registry)

		// Use the content-specific prompt variant
		contentPrompt := prompts.GetContentPrompt("", format, "", "")
		contentPrompt += fmt.Sprintf("\n\nOutput format: %s\n", format)

		// Inject workspace grimoire if available (project-specific rules/context)
		cwd, _ := os.Getwd()
		if cwd != "" {
			if projectGrimoire, err := grimoire.LoadAll(cwd); err == nil && projectGrimoire != nil && !projectGrimoire.IsEmpty() {
				contentPrompt += "\n\n# Project Context (.grimoire)\n\n" + projectGrimoire.Render()
			}
		}

		client.SetSystemPrompt(contentPrompt)

		messages := []tui.ChatMessage{
			{Role: "user", Content: prompt, Timestamp: time.Now()},
		}

		result, err := client.SendMessageSync(ctx, messages, nil)
		if err != nil {
			return nil, fmt.Errorf("content generation error: %w", err)
		}

		return []ContentBlock{{Type: "text", Text: strings.TrimSpace(result.Content)}}, nil
	})
}

// --- celeste_status tool ---

func celesteStatusToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_status",
		Description: "Get Celeste's current status: connected providers, loaded grimoire, indexed project, session cost.",
		InputSchema: schema,
	}
}

func registerCelesteStatusTool(s *Server) {
	startTime := time.Now()

	s.RegisterTool(celesteStatusToolDef(), func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		cfg := s.config.CelesteConfig

		status := map[string]any{
			"server":  serverName,
			"version": serverVersion,
			"uptime":  time.Since(startTime).Round(time.Second).String(),
			"health":  "ok",
		}

		if cfg != nil {
			status["provider"] = cfg.BaseURL
			status["model"] = cfg.Model
		}

		status["workspace"] = s.config.Workspace
		status["transport"] = s.config.Transport

		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal status: %w", err)
		}

		return []ContentBlock{{Type: "text", Text: string(data)}}, nil
	})
}
