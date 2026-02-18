// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sashabaranov/go-openai"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// OpenAIBackend implements LLMBackend using the go-openai SDK.
// This backend supports OpenAI, Grok, Venice, Anthropic, and other OpenAI-compatible providers.
type OpenAIBackend struct {
	client       *openai.Client
	config       *Config
	systemPrompt string
}

// NewOpenAIBackend creates a new OpenAI-compatible backend.
func NewOpenAIBackend(config *Config) *OpenAIBackend {
	clientConfig := openai.DefaultConfig(config.APIKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

	return &OpenAIBackend{
		client: openai.NewClientWithConfig(clientConfig),
		config: config,
	}
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (b *OpenAIBackend) SetSystemPrompt(prompt string) {
	b.systemPrompt = prompt
}

// SendMessageSync sends a message synchronously and returns the complete result.
func (b *OpenAIBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	// Convert messages to OpenAI format
	openAIMessages := b.convertMessages(messages)

	// Convert tools to OpenAI format
	openAITools := b.convertTools(tools)

	// Create request
	req := openai.ChatCompletionRequest{
		Model:    b.config.Model,
		Messages: openAIMessages,
		Stream:   true,
	}

	if len(openAITools) > 0 {
		req.Tools = openAITools
	}

	// Create streaming request
	stream, err := b.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	result := &ChatCompletionResult{}
	var toolCalls []openai.ToolCall

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			result.Error = err
			return result, err
		}

		for _, choice := range response.Choices {
			// Handle content delta
			if choice.Delta.Content != "" {
				result.Content += choice.Delta.Content
			}

			// Handle tool calls
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index != nil {
					idx := *tc.Index
					for len(toolCalls) <= idx {
						toolCalls = append(toolCalls, openai.ToolCall{})
					}
					if tc.ID != "" {
						toolCalls[idx].ID = tc.ID
					}
					if tc.Type != "" {
						toolCalls[idx].Type = tc.Type
					}
					if tc.Function.Name != "" {
						toolCalls[idx].Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						toolCalls[idx].Function.Arguments += tc.Function.Arguments
					}
				}
			}

			// Check finish reason
			if choice.FinishReason != "" {
				result.FinishReason = string(choice.FinishReason)
			}
		}
	}

	// Convert tool calls
	for _, tc := range toolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCallResult{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return result, nil
}

// SendMessageStream sends a message with streaming callback.
func (b *OpenAIBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	// Convert messages to OpenAI format
	openAIMessages := b.convertMessages(messages)

	// Convert tools to OpenAI format
	openAITools := b.convertTools(tools)

	// Create request
	req := openai.ChatCompletionRequest{
		Model:    b.config.Model,
		Messages: openAIMessages,
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	if len(openAITools) > 0 {
		req.Tools = openAITools
	}

	// Create streaming request
	stream, err := b.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	var toolCalls []openai.ToolCall
	var usage *TokenUsage
	isFirst := true

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// Send final chunk with usage data if available
			callback(StreamChunk{
				IsFinal:      true,
				FinishReason: "stop",
				ToolCalls:    convertToolCalls(toolCalls),
				Usage:        usage,
			})
			return nil
		}
		if err != nil {
			return err
		}

		// Capture usage data from response (only in final chunk with StreamOptions)
		if response.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     response.Usage.PromptTokens,
				CompletionTokens: response.Usage.CompletionTokens,
				TotalTokens:      response.Usage.TotalTokens,
			}
		}

		for _, choice := range response.Choices {
			chunk := StreamChunk{
				IsFirst: isFirst,
			}

			// Handle content delta
			if choice.Delta.Content != "" {
				chunk.Content = choice.Delta.Content
			}

			// Handle tool calls
			// Note: Different providers stream tool calls in different formats:
			// - OpenAI: Streams tool calls incrementally across multiple chunks with an Index
			// - Gemini (via OpenAI compat): Sends complete tool calls in a single chunk without an Index
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index != nil {
					// OpenAI format: Tool calls have an index for streaming accumulation
					idx := *tc.Index
					for len(toolCalls) <= idx {
						toolCalls = append(toolCalls, openai.ToolCall{})
					}
					if tc.ID != "" {
						toolCalls[idx].ID = tc.ID
					}
					if tc.Type != "" {
						toolCalls[idx].Type = tc.Type
					}
					if tc.Function.Name != "" {
						toolCalls[idx].Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						toolCalls[idx].Function.Arguments += tc.Function.Arguments
					}
				} else {
					// Gemini/other format: Tool calls come complete without an index
					// Append as a new tool call if it has an ID
					if tc.ID != "" {
						toolCalls = append(toolCalls, openai.ToolCall{
							ID:   tc.ID,
							Type: tc.Type,
							Function: openai.FunctionCall{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						})
					}
				}
			}

			// Check finish reason
			if choice.FinishReason != "" {
				chunk.IsFinal = true
				chunk.FinishReason = string(choice.FinishReason)
				chunk.ToolCalls = convertToolCalls(toolCalls)
			}

			// Call callback
			callback(chunk)
			isFirst = false
		}
	}
}

// Close cleans up resources (no-op for OpenAI backend).
func (b *OpenAIBackend) Close() error {
	return nil
}

// convertMessages converts TUI messages to OpenAI format.
func (b *OpenAIBackend) convertMessages(messages []tui.ChatMessage) []openai.ChatCompletionMessage {
	var result []openai.ChatCompletionMessage

	// Add system prompt if configured
	if b.systemPrompt != "" && !b.config.SkipPersonaPrompt {
		result = append(result, openai.ChatCompletionMessage{
			Role:    "system",
			Content: b.systemPrompt,
		})
	}

	// Convert messages
	for _, msg := range messages {
		// Skip messages with empty content (except tool calls which can have empty content)
		if msg.Content == "" && len(msg.ToolCalls) == 0 && msg.Role != "tool" {
			// Skip empty messages to prevent API errors (Grok requires content field)
			continue
		}

		if msg.Role == "tool" {
			// Tool messages need special format with tool_call_id
			result = append(result, openai.ChatCompletionMessage{
				Role:       "tool",
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
			})
		} else if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Assistant messages with tool_calls need to include ToolCalls field
			toolCalls := make([]openai.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCalls[i] = openai.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}

			// For tool-calling messages, ensure content is at least empty string (not nil)
			content := msg.Content
			if content == "" {
				content = "" // Explicitly set to empty string for serialization
			}

			result = append(result, openai.ChatCompletionMessage{
				Role:      msg.Role,
				Content:   content,
				ToolCalls: toolCalls,
			})
		} else {
			// Regular messages (user, assistant without tool_calls, system)
			result = append(result, openai.ChatCompletionMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return result
}

// convertTools converts TUI skill definitions to OpenAI tools.
func (b *OpenAIBackend) convertTools(tools []tui.SkillDefinition) []openai.Tool {
	var result []openai.Tool

	// Add xAI built-in tools if configured (xAI only)
	if b.config.Collections != nil && b.config.Collections.Enabled {
		if len(b.config.Collections.ActiveCollections) > 0 {
			// Note: collections_search is a built-in xAI tool
			// The go-openai library might not have this type yet
			// We'll need to use a generic tool structure
			result = append(result, openai.Tool{
				Type: "collections_search",
				// Collections IDs are passed in the tool definition for xAI
			})
		}
	}

	// Add web_search if enabled (xAI only)
	if b.config.XAIFeatures != nil && b.config.XAIFeatures.EnableWebSearch {
		result = append(result, openai.Tool{
			Type: "web_search",
		})
	}

	// Add x_search if enabled (xAI only)
	if b.config.XAIFeatures != nil && b.config.XAIFeatures.EnableXSearch {
		result = append(result, openai.Tool{
			Type: "x_search",
		})
	}

	// Add user-defined function tools
	for _, tool := range tools {
		params, _ := json.Marshal(tool.Parameters)

		result = append(result, openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  json.RawMessage(params),
			},
		})
	}

	return result
}

// convertToolCalls converts OpenAI tool calls to result format.
func convertToolCalls(toolCalls []openai.ToolCall) []ToolCallResult {
	var result []ToolCallResult
	for _, tc := range toolCalls {
		result = append(result, ToolCallResult{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return result
}
