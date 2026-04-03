package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

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
	client.SetSystemPrompt(systemPrompt)

	messages := []tui.ChatMessage{
		{Role: "user", Content: prompt, Timestamp: time.Now()},
	}

	result, err := client.SendMessageSync(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("chat error: %w", err)
	}

	return []ContentBlock{{Type: "text", Text: strings.TrimSpace(result.Content)}}, nil
}

// runAgentMode runs a multi-turn agent loop for complex tasks.
func runAgentMode(ctx context.Context, cfg *config.Config, goal, workspace string) ([]ContentBlock, error) {
	var outBuf, errBuf bytes.Buffer

	opts := agent.Options{
		Workspace: workspace,
		MaxTurns:  20,
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

	// Collect the final assistant response
	response := state.LastAssistantResponse
	if response == "" && outBuf.Len() > 0 {
		response = outBuf.String()
	}
	if response == "" {
		response = fmt.Sprintf("Agent completed (%s) after %d turns", state.Status, state.Turn)
	}

	return []ContentBlock{{Type: "text", Text: response}}, nil
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
