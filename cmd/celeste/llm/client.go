// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Client wraps LLM backends and provides a unified interface.
// It automatically selects the appropriate backend (OpenAI or Google) based on the provider.
type Client struct {
	backend      LLMBackend
	config       *Config
	registry     *skills.Registry
	backendType  BackendType
	systemPrompt string
}

// Config holds LLM client configuration.
type Config struct {
	APIKey            string
	BaseURL           string
	Model             string
	Timeout           time.Duration
	SkipPersonaPrompt bool
	SimulateTyping    bool
	TypingSpeed       int // chars per second

	// Google Cloud authentication (for Gemini/Vertex AI)
	GoogleCredentialsFile string // Path to service account JSON file
	GoogleUseADC          bool   // Use Application Default Credentials

	// Collections (xAI only)
	Collections *config.CollectionsConfig
	XAIFeatures *config.XAIFeaturesConfig
}

// NewClient creates a new LLM client with automatic backend selection.
// It detects whether to use OpenAI SDK, Google GenAI SDK, or xAI SDK based on the base URL.
func NewClient(config *Config, registry *skills.Registry) *Client {
	// Detect which backend to use
	backendType := DetectBackendType(config.BaseURL)

	var backend LLMBackend
	switch backendType {
	case BackendTypeXAI:
		// Use native xAI SDK for Grok with Collections support
		xaiBackend, err := NewXAIBackend(config, registry)
		if err != nil {
			// Fallback to OpenAI backend if xAI backend fails
			fmt.Fprintf(os.Stderr, "Warning: Failed to create xAI backend: %v\nFalling back to OpenAI SDK\n", err)
			backendType = BackendTypeOpenAI
			backend = NewOpenAIBackend(config)
		} else {
			backend = xaiBackend
			tui.LogInfo("Using xAI backend with Collections support")
		}

	case BackendTypeGoogle:
		// Use Google GenAI SDK for Gemini/Vertex AI
		googleBackend, err := NewGoogleBackend(config)
		if err != nil {
			// Fallback to OpenAI backend if Google backend fails
			fmt.Fprintf(os.Stderr, "Warning: Failed to create Google backend: %v\nFalling back to OpenAI SDK\n", err)
			backendType = BackendTypeOpenAI
			backend = NewOpenAIBackend(config)
		} else {
			backend = googleBackend
		}

	default:
		// Use OpenAI SDK for OpenAI, Venice, Anthropic, etc.
		backend = NewOpenAIBackend(config)
	}

	return &Client{
		backend:     backend,
		config:      config,
		registry:    registry,
		backendType: backendType,
	}
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (c *Client) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt
	if c.backend != nil {
		c.backend.SetSystemPrompt(prompt)
	}
}

// UpdateConfig updates the client configuration and recreates the backend if needed.
// This allows dynamic endpoint/model switching during runtime.
func (c *Client) UpdateConfig(config *Config) {
	c.config = config

	// Detect if backend type changed
	newBackendType := DetectBackendType(config.BaseURL)

	if newBackendType != c.backendType {
		// Backend type changed - recreate backend
		if c.backend != nil {
			c.backend.Close()
		}

		switch newBackendType {
		case BackendTypeXAI:
			xaiBackend, err := NewXAIBackend(config, c.registry)
			if err != nil {
				// Fallback to OpenAI if xAI fails
				fmt.Fprintf(os.Stderr, "Warning: Failed to create xAI backend: %v\nFalling back to OpenAI SDK\n", err)
				newBackendType = BackendTypeOpenAI
				c.backend = NewOpenAIBackend(config)
			} else {
				c.backend = xaiBackend
				tui.LogInfo("Switched to xAI backend with Collections support")
			}

		case BackendTypeGoogle:
			googleBackend, err := NewGoogleBackend(config)
			if err != nil {
				// Fallback to OpenAI if Google fails
				fmt.Fprintf(os.Stderr, "Warning: Failed to create Google backend: %v\nFalling back to OpenAI SDK\n", err)
				newBackendType = BackendTypeOpenAI
				c.backend = NewOpenAIBackend(config)
			} else {
				c.backend = googleBackend
			}

		default:
			c.backend = NewOpenAIBackend(config)
		}

		c.backendType = newBackendType

		// Restore system prompt
		if c.systemPrompt != "" {
			c.backend.SetSystemPrompt(c.systemPrompt)
		}
	}
	// Note: Config changes within same backend type are handled by passing config to methods
}

// GetConfig returns the current configuration.
func (c *Client) GetConfig() *Config {
	return c.config
}

// ChatCompletionResult holds the result of a chat completion.
type ChatCompletionResult struct {
	Content      string
	ToolCalls    []ToolCallResult
	FinishReason string
	Error        error
}

// ToolCallResult holds a tool call from the LLM.
type ToolCallResult struct {
	ID        string
	Name      string
	Arguments string
}

// SendMessageSync sends a message synchronously and returns the result.
// This delegates to the appropriate backend (OpenAI or Google).
func (c *Client) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	return c.backend.SendMessageSync(ctx, messages, tools)
}

// StreamCallback is called for each chunk during streaming.
type StreamCallback func(chunk StreamChunk)

// StreamChunk represents a streaming chunk.
// TokenUsage holds token usage information from API response
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type StreamChunk struct {
	Content      string
	IsFirst      bool
	IsFinal      bool
	FinishReason string
	ToolCalls    []ToolCallResult
	Usage        *TokenUsage // Only populated on final chunk with stream_options
}

// SendMessageStream sends a message with streaming callback.
// This delegates to the appropriate backend (OpenAI or Google).
func (c *Client) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	return c.backend.SendMessageStream(ctx, messages, tools, callback)
}

// GetSkills returns skill definitions for the TUI.
func (c *Client) GetSkills() []tui.SkillDefinition {
	if c.registry == nil {
		return nil
	}

	allSkills := c.registry.GetAllSkills()
	var result []tui.SkillDefinition

	for _, skill := range allSkills {
		result = append(result, tui.SkillDefinition{
			Name:        skill.Name,
			Description: skill.Description,
			Parameters:  skill.Parameters,
		})
	}

	return result
}

// ExecuteSkill executes a skill and returns the result.
func (c *Client) ExecuteSkill(ctx context.Context, name string, argsJSON string) (*skills.ExecutionResult, error) {
	if c.registry == nil {
		return nil, fmt.Errorf("no skill registry configured")
	}

	executor := skills.NewExecutor(c.registry)
	return executor.Execute(ctx, name, argsJSON)
}
