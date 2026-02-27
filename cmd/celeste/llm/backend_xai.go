// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// XAIBackend implements LLMBackend using xAI's native API.
// This backend supports xAI-specific features like Collections (RAG).
type XAIBackend struct {
	apiKey       string
	baseURL      string
	model        string
	config       *Config
	httpClient   *http.Client
	systemPrompt string
	registry     *skills.Registry
}

// NewXAIBackend creates a new xAI backend with Collections support.
func NewXAIBackend(config *Config, registry *skills.Registry) (*XAIBackend, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("xAI API key is required")
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.x.ai/v1"
	}

	return &XAIBackend{
		apiKey:     config.APIKey,
		baseURL:    baseURL,
		model:      config.Model,
		config:     config,
		registry:   registry,
		httpClient: &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}, nil
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (b *XAIBackend) SetSystemPrompt(prompt string) {
	b.systemPrompt = prompt
}

// xAIMessage represents a message in xAI's format
type xAIMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []xAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

// xAIToolCall represents a tool call in xAI's format
type xAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// xAITool represents a function tool in xAI's format
type xAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// xAIChatCompletionRequest is the request format for xAI chat completions
type xAIChatCompletionRequest struct {
	Model         string       `json:"model"`
	Messages      []xAIMessage `json:"messages"`
	Tools         []xAITool    `json:"tools,omitempty"`
	Stream        bool         `json:"stream"`
	CollectionIDs []string     `json:"collection_ids,omitempty"` // xAI Collections support
	Temperature   float32      `json:"temperature,omitempty"`
	MaxTokens     int          `json:"max_tokens,omitempty"`
}

// xAIStreamChunk represents a streaming response chunk
type xAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string        `json:"role,omitempty"`
			Content   string        `json:"content,omitempty"`
			ToolCalls []xAIToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		NumSourcesUsed   int `json:"num_sources_used,omitempty"` // xAI Collections indicator
	} `json:"usage,omitempty"`
}

// SendMessageStream sends a message with streaming callback.
func (b *XAIBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	// Convert messages to xAI format
	xaiMessages := b.convertMessages(messages)

	// Convert tools to xAI format
	xaiTools := b.convertTools(tools)

	// Build request
	req := xAIChatCompletionRequest{
		Model:    b.model,
		Messages: xaiMessages,
		Tools:    xaiTools,
		Stream:   true,
	}

	// Add Collections support if enabled
	if b.config.Collections != nil && b.config.Collections.Enabled {
		if len(b.config.Collections.ActiveCollections) > 0 {
			req.CollectionIDs = b.config.Collections.ActiveCollections
			tui.LogInfo(fmt.Sprintf("xAI Collections enabled: %d collections active: %v",
				len(req.CollectionIDs), req.CollectionIDs))
		}
	}

	// Marshal request
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		b.baseURL+"/chat/completions",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)

	// Send request
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("xAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Process streaming response
	scanner := bufio.NewScanner(resp.Body)
	var toolCalls []xAIToolCall
	var usage *TokenUsage
	isFirst := true

	for scanner.Scan() {
		line := scanner.Text()
		if !bytes.HasPrefix([]byte(line), []byte("data: ")) {
			continue
		}

		data := bytes.TrimPrefix([]byte(line), []byte("data: "))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}

		var chunk xAIStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			tui.LogInfo(fmt.Sprintf("Warning: failed to parse chunk: %v", err))
			continue
		}

		// Process choices
		for _, choice := range chunk.Choices {
			// Send content delta
			if choice.Delta.Content != "" {
				callback(StreamChunk{
					Content: choice.Delta.Content,
					IsFirst: isFirst,
				})
				isFirst = false
			}

			// Accumulate tool calls
			for _, tc := range choice.Delta.ToolCalls {
				// Update or append tool call
				found := false
				for i := range toolCalls {
					if toolCalls[i].ID == tc.ID {
						toolCalls[i] = tc
						found = true
						break
					}
				}
				if !found {
					toolCalls = append(toolCalls, tc)
				}
			}

			// Handle finish reason
			if choice.FinishReason != "" {
				// Convert tool calls if present
				var convertedToolCalls []ToolCallResult
				if len(toolCalls) > 0 {
					for _, tc := range toolCalls {
						convertedToolCalls = append(convertedToolCalls, ToolCallResult{
							ID:        tc.ID,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						})
					}
				}

				callback(StreamChunk{
					IsFinal:      true,
					FinishReason: choice.FinishReason,
					ToolCalls:    convertedToolCalls,
					Usage:        usage,
				})
			}
		}

		// Capture usage stats (typically in final chunk)
		if chunk.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}

			// Log Collections usage
			if chunk.Usage.NumSourcesUsed > 0 {
				tui.LogInfo(fmt.Sprintf("✅ xAI Collections: %d sources used in response", chunk.Usage.NumSourcesUsed))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}

	return nil
}

// SendMessageSync sends a message synchronously (not implemented for xAI backend).
func (b *XAIBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	return nil, fmt.Errorf("SendMessageSync not implemented for xAI backend, use SendMessageStream instead")
}

// convertMessages converts TUI messages to xAI format
func (b *XAIBackend) convertMessages(messages []tui.ChatMessage) []xAIMessage {
	var result []xAIMessage

	// Add system prompt if set
	if b.systemPrompt != "" {
		result = append(result, xAIMessage{
			Role:    "system",
			Content: b.systemPrompt,
		})
	}

	// Convert messages
	for _, msg := range messages {
		if msg.Role == "tool" {
			// Tool response
			result = append(result, xAIMessage{
				Role:       "tool",
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
				Name:       msg.Name,
			})
		} else if len(msg.ToolCalls) > 0 {
			// Assistant message with tool calls
			var toolCalls []xAIToolCall
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, xAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}

			result = append(result, xAIMessage{
				Role:      msg.Role,
				Content:   msg.Content,
				ToolCalls: toolCalls,
			})
		} else {
			// Regular message
			result = append(result, xAIMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return result
}

// convertTools converts TUI skill definitions to xAI tools
func (b *XAIBackend) convertTools(tools []tui.SkillDefinition) []xAITool {
	var result []xAITool

	for _, tool := range tools {
		params, err := json.Marshal(tool.Parameters)
		if err != nil {
			tui.LogInfo(fmt.Sprintf("Skipping invalid tool '%s': failed to marshal parameters: %v", tool.Name, err))
			continue
		}

		xaiTool := xAITool{
			Type: "function",
		}
		xaiTool.Function.Name = tool.Name
		xaiTool.Function.Description = tool.Description
		xaiTool.Function.Parameters = json.RawMessage(params)

		result = append(result, xaiTool)
	}

	return result
}

// SwitchEndpoint switches to a different endpoint (for config switching)
func (b *XAIBackend) SwitchEndpoint(endpoint string) error {
	// For xAI backend, we don't support switching to other providers
	// This backend is xAI-specific
	return fmt.Errorf("xAI backend cannot switch to other providers")
}

// ChangeModel changes the model
func (b *XAIBackend) ChangeModel(model string) error {
	b.model = model
	tui.LogInfo(fmt.Sprintf("xAI backend model changed to: %s", model))
	return nil
}

// GetSkills returns the list of available skills from the registry
func (b *XAIBackend) GetSkills() []tui.SkillDefinition {
	if b.registry == nil {
		return []tui.SkillDefinition{}
	}

	skillsList := b.registry.GetAllSkills()
	result := make([]tui.SkillDefinition, 0, len(skillsList))

	for _, skill := range skillsList {
		result = append(result, tui.SkillDefinition{
			Name:        skill.Name,
			Description: skill.Description,
			Parameters:  skill.Parameters,
		})
	}

	return result
}

// Close cleans up resources (implements LLMBackend interface)
func (b *XAIBackend) Close() error {
	// HTTP client doesn't need explicit cleanup
	return nil
}
