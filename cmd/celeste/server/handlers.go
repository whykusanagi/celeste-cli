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

// RegisterHandlers registers all three MCP tool handlers on the server.
func RegisterHandlers(s *Server) {
	registerCelesteTool(s)
	registerCelesteContentTool(s)
	registerCelesteStatusTool(s)
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

	// Auto-loop: send message, execute tool calls, send results back, repeat
	maxLoops := 25
	for i := 0; i < maxLoops; i++ {
		result, err := client.SendMessageSync(ctx, messages, toolDefs)
		if err != nil {
			return nil, fmt.Errorf("chat error: %w", err)
		}

		// If no tool calls, we're done
		if len(result.ToolCalls) == 0 {
			return []ContentBlock{{Type: "text", Text: strings.TrimSpace(result.Content)}}, nil
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

		// Execute each tool call and add results
		for _, tc := range result.ToolCalls {
			// Parse JSON arguments string to map
			var args map[string]any
			if tc.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Arguments), &args)
			}
			if args == nil {
				args = make(map[string]any)
			}
			toolResult, _ := registry.Execute(ctx, tc.Name, args)
			messages = append(messages, tui.ChatMessage{
				Role:       "tool",
				Content:    toolResult.Content,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
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
