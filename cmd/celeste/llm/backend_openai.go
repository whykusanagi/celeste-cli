// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"

	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// OpenAIBackend implements LLMBackend using the go-openai SDK.
// This backend supports OpenAI, Grok, Venice, Anthropic, and other OpenAI-compatible providers.
type OpenAIBackend struct {
	client         *openai.Client
	config         *Config
	systemPrompt   string
	thinkingConfig ThinkingConfig
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

// SetThinkingConfig configures extended thinking / reasoning effort.
// For OpenAI o-series models this maps to reasoning_effort.
// For Anthropic (via OpenAI compat) this is a no-op for now.
func (b *OpenAIBackend) SetThinkingConfig(config ThinkingConfig) {
	b.thinkingConfig = config
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
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	if len(openAITools) > 0 {
		req.Tools = openAITools
		req.ToolChoice = "auto"
	}

	b.applyThinkingConfig(&req)

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

		// Capture usage from the final usage-only chunk (sent by OpenAI when IncludeUsage is true).
		if response.Usage != nil {
			result.Usage = &TokenUsage{
				PromptTokens:     response.Usage.PromptTokens,
				CompletionTokens: response.Usage.CompletionTokens,
				TotalTokens:      response.Usage.TotalTokens,
			}
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

	b.applyThinkingConfig(&req)

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

// SendMessageStreamEvents sends a message with granular streaming events.
func (b *OpenAIBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
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

	b.applyThinkingConfig(&req)

	// Create streaming request
	stream, err := b.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Track tool calls by index for accumulation
	type toolCallState struct {
		id   string
		name string
		args string
	}
	var toolCallsByIndex []toolCallState
	var usage *TokenUsage
	var finishReason string

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		// Capture usage from the final usage-only chunk
		if response.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     response.Usage.PromptTokens,
				CompletionTokens: response.Usage.CompletionTokens,
				TotalTokens:      response.Usage.TotalTokens,
			}
		}

		for _, choice := range response.Choices {
			// Handle content delta
			if choice.Delta.Content != "" {
				callback(StreamEvent{
					Type:         EventContentDelta,
					ContentDelta: choice.Delta.Content,
				})
			}

			// Handle tool calls
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index != nil {
					idx := *tc.Index
					// New tool call index → emit ToolUseStart
					if idx >= len(toolCallsByIndex) {
						for len(toolCallsByIndex) <= idx {
							toolCallsByIndex = append(toolCallsByIndex, toolCallState{})
						}
						toolCallsByIndex[idx].id = tc.ID
						toolCallsByIndex[idx].name = tc.Function.Name
						callback(StreamEvent{
							Type:      EventToolUseStart,
							ToolUseID: tc.ID,
							ToolName:  tc.Function.Name,
						})
					} else {
						// Update ID/name if provided in subsequent chunks
						if tc.ID != "" {
							toolCallsByIndex[idx].id = tc.ID
						}
						if tc.Function.Name != "" {
							toolCallsByIndex[idx].name = tc.Function.Name
						}
					}

					// Accumulate arguments and emit input delta
					if tc.Function.Arguments != "" {
						toolCallsByIndex[idx].args += tc.Function.Arguments
						callback(StreamEvent{
							Type:       EventToolUseInputDelta,
							ToolUseID:  toolCallsByIndex[idx].id,
							InputDelta: tc.Function.Arguments,
						})
					}
				} else {
					// Gemini/other format: complete tool call without index
					if tc.ID != "" {
						callback(StreamEvent{
							Type:      EventToolUseStart,
							ToolUseID: tc.ID,
							ToolName:  tc.Function.Name,
						})
						callback(StreamEvent{
							Type:          EventToolUseDone,
							ToolUseID:     tc.ID,
							ToolName:      tc.Function.Name,
							CompleteInput: tc.Function.Arguments,
						})
					}
				}
			}

			// Check finish reason
			if choice.FinishReason != "" {
				finishReason = string(choice.FinishReason)
			}
		}
	}

	// Emit ToolUseDone for each accumulated indexed tool call
	for _, tc := range toolCallsByIndex {
		if tc.id != "" {
			callback(StreamEvent{
				Type:          EventToolUseDone,
				ToolUseID:     tc.id,
				ToolName:      tc.name,
				CompleteInput: tc.args,
			})
		}
	}

	// Emit MessageDone
	if finishReason == "" {
		finishReason = "stop"
	}
	callback(StreamEvent{
		Type:         EventMessageDone,
		Usage:        usage,
		FinishReason: finishReason,
	})

	return nil
}

// applyThinkingConfig adds reasoning_effort to the request when the model
// supports it (OpenAI o-series) and thinking is enabled.
func (b *OpenAIBackend) applyThinkingConfig(req *openai.ChatCompletionRequest) {
	if !b.thinkingConfig.Enabled || b.thinkingConfig.Level == "off" {
		return
	}
	// OpenAI o-series models support reasoning_effort ("low", "medium", "high").
	// Map our extended levels into what the API accepts.
	model := strings.ToLower(req.Model)
	if !strings.HasPrefix(model, "o1") && !strings.HasPrefix(model, "o3") && !strings.HasPrefix(model, "o4") {
		return // Not an o-series model; skip silently
	}
	switch b.thinkingConfig.Level {
	case "low":
		req.ReasoningEffort = "low"
	case "medium":
		req.ReasoningEffort = "medium"
	case "high", "max":
		req.ReasoningEffort = "high"
	}
}

// Close cleans up resources (no-op for OpenAI backend).
func (b *OpenAIBackend) Close() error {
	return nil
}

// convertMessages converts TUI messages to OpenAI format.
func (b *OpenAIBackend) convertMessages(messages []tui.ChatMessage) []openai.ChatCompletionMessage {
	var result []openai.ChatCompletionMessage

	// Add system prompt if set. The SkipPersonaPrompt flag controls whether the
	// Celeste VTuber persona is prepended (handled upstream in runtime.go), not
	// whether the system prompt itself is omitted — so we always include it here.
	//
	// TODO(prompt-caching): When using Anthropic via OpenAI compat, structure
	// the system message with cache_control hints for prompt caching. The static
	// prefix of the CacheablePrompt should include:
	//   {"type": "text", "text": "<static>", "cache_control": {"type": "ephemeral"}}
	// This requires switching from a simple string content to multi-part content
	// blocks when b.isAnthropicProvider() is true.
	if b.systemPrompt != "" {
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

			// If this tool result carries image metadata, inject a user
			// message with the image as a data URL so vision-capable models
			// can actually see it.  The OpenAI API only supports multipart
			// content on user messages, not tool messages.
			if msg.Metadata != nil {
				if imgType, ok := msg.Metadata["type"].(string); ok && imgType == "image" {
					if b64, ok := msg.Metadata["base64"].(string); ok {
						format, _ := msg.Metadata["format"].(string)
						if format == "" {
							format = "png"
						}
						filename, _ := msg.Metadata["filename"].(string)
						dataURL := fmt.Sprintf("data:image/%s;base64,%s", format, b64)
						result = append(result, openai.ChatCompletionMessage{
							Role: "user",
							MultiContent: []openai.ChatMessagePart{
								{
									Type: openai.ChatMessagePartTypeText,
									Text: fmt.Sprintf("[Attached image from tool result: %s]", filename),
								},
								{
									Type: openai.ChatMessagePartTypeImageURL,
									ImageURL: &openai.ChatMessageImageURL{
										URL:    dataURL,
										Detail: openai.ImageURLDetailAuto,
									},
								},
							},
						})
					}
				}
			}
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
// Note: xAI-specific tool types (collections_search, web_search, x_search) are
// handled exclusively by the XAIBackend; this backend is for OpenAI-compatible
// endpoints only and must not inject non-standard tool types.
func (b *OpenAIBackend) convertTools(tools []tui.SkillDefinition) []openai.Tool {
	var result []openai.Tool

	// Add user-defined function tools
	for _, tool := range tools {
		params, err := json.Marshal(tool.Parameters)
		if err != nil {
			tui.LogInfo(fmt.Sprintf("Skipping invalid tool '%s': failed to marshal parameters: %v", tool.Name, err))
			continue
		}

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
