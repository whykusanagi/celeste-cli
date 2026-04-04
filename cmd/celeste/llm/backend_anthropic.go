// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// AnthropicBackend implements LLMBackend using the native Anthropic SDK.
// This backend supports Claude models via the Anthropic Messages API with
// prompt caching, extended thinking, and native streaming.
type AnthropicBackend struct {
	client         *anthropic.Client
	config         *Config
	systemPrompt   string
	thinkingConfig ThinkingConfig
}

// NewAnthropicBackend creates a new Anthropic backend using the native SDK.
func NewAnthropicBackend(config *Config) (*AnthropicBackend, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
	}
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}

	client := anthropic.NewClient(opts...)

	return &AnthropicBackend{
		client: &client,
		config: config,
	}, nil
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (b *AnthropicBackend) SetSystemPrompt(prompt string) {
	b.systemPrompt = prompt
}

// SetThinkingConfig configures extended thinking for Claude models.
// Claude supports budget_tokens via ThinkingConfigParam.
func (b *AnthropicBackend) SetThinkingConfig(config ThinkingConfig) {
	b.thinkingConfig = config
}

// Close cleans up resources (no-op for Anthropic backend).
func (b *AnthropicBackend) Close() error {
	return nil
}

// maxTokens returns the max_tokens value for the request.
// When thinking is enabled, Anthropic requires a higher max_tokens that
// encompasses both thinking and output tokens.
func (b *AnthropicBackend) maxTokens() int64 {
	if b.thinkingConfig.Enabled && b.thinkingConfig.Level != "off" {
		budget := b.thinkingConfig.LevelToBudget()
		if budget > 0 {
			// max_tokens must be > budget_tokens; add generous room for output
			return int64(budget) + 16384
		}
		return 65536 // sensible default when thinking is on
	}
	return 8192 // default for non-thinking requests
}

// buildParams constructs the MessageNewParams shared by sync and streaming requests.
func (b *AnthropicBackend) buildParams(messages []tui.ChatMessage, tools []tui.SkillDefinition) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:    anthropic.Model(b.config.Model),
		MaxTokens: b.maxTokens(),
		Messages: b.convertMessages(messages),
	}

	// Set system prompt with cache control on the static prefix.
	if b.systemPrompt != "" {
		params.System = b.buildSystemBlocks(b.systemPrompt)
	}

	// Convert tools.
	anthropicTools := b.convertTools(tools)
	if len(anthropicTools) > 0 {
		params.Tools = anthropicTools
		params.ToolChoice = anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		}
	}

	// Apply thinking config.
	b.applyThinkingConfig(&params)

	return params
}

// buildSystemBlocks creates system prompt text blocks with prompt caching.
// The system prompt is split: the first block gets cache_control for
// Anthropic's prompt caching, so the static persona/grimoire stays cached
// across turns.
func (b *AnthropicBackend) buildSystemBlocks(prompt string) []anthropic.TextBlockParam {
	// Try to split on the separator used by CacheablePrompt.FullPrompt().
	separator := "\n\n---\n\n"
	if idx := strings.Index(prompt, separator); idx > 0 {
		staticPrefix := prompt[:idx]
		dynamicSuffix := prompt[idx+len(separator):]

		blocks := []anthropic.TextBlockParam{
			{
				Text:         staticPrefix,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			},
		}
		if dynamicSuffix != "" {
			blocks = append(blocks, anthropic.TextBlockParam{
				Text: dynamicSuffix,
			})
		}
		return blocks
	}

	// No separator found — single block with cache control.
	return []anthropic.TextBlockParam{
		{
			Text:         prompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		},
	}
}

// applyThinkingConfig adds extended thinking parameters when enabled.
func (b *AnthropicBackend) applyThinkingConfig(params *anthropic.MessageNewParams) {
	if !b.thinkingConfig.Enabled || b.thinkingConfig.Level == "off" {
		return
	}

	budget := b.thinkingConfig.LevelToBudget()
	if budget <= 0 {
		budget = 8192 // default budget
	}

	params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
}

// SendMessageSync sends a message and returns the complete result.
func (b *AnthropicBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	params := b.buildParams(messages, tools)

	// Use streaming internally to accumulate the full response, matching
	// the pattern used by the OpenAI backend for consistency.
	stream := b.client.Messages.NewStreaming(ctx, params)

	result := &ChatCompletionResult{}
	var toolCalls []ToolCallResult

	// Track content blocks by index to accumulate tool call input JSON.
	type blockState struct {
		blockType string
		id        string
		name      string
		inputJSON string
	}
	blocks := make(map[int64]*blockState)

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_start":
			cb := event.ContentBlock
			bs := &blockState{blockType: cb.Type}
			switch cb.Type {
			case "tool_use":
				bs.id = cb.ID
				bs.name = cb.Name
			}
			blocks[event.Index] = bs

		case "content_block_delta":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			switch bs.blockType {
			case "text":
				result.Content += event.Delta.Text
			case "tool_use":
				bs.inputJSON += event.Delta.PartialJSON
			}

		case "content_block_stop":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			if bs.blockType == "tool_use" {
				toolCalls = append(toolCalls, ToolCallResult{
					ID:        bs.id,
					Name:      bs.name,
					Arguments: bs.inputJSON,
				})
			}

		case "message_delta":
			if event.Delta.StopReason != "" {
				result.FinishReason = mapStopReason(string(event.Delta.StopReason))
			}
			if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 {
				result.Usage = &TokenUsage{
					PromptTokens:     int(event.Usage.InputTokens),
					CompletionTokens: int(event.Usage.OutputTokens),
					TotalTokens:      int(event.Usage.InputTokens + event.Usage.OutputTokens),
				}
			}

		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				result.Usage = &TokenUsage{
					PromptTokens:     int(event.Message.Usage.InputTokens),
					CompletionTokens: int(event.Message.Usage.OutputTokens),
					TotalTokens:      int(event.Message.Usage.InputTokens + event.Message.Usage.OutputTokens),
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	result.ToolCalls = toolCalls
	return result, nil
}

// SendMessageStream sends a message with streaming callback.
func (b *AnthropicBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	params := b.buildParams(messages, tools)

	stream := b.client.Messages.NewStreaming(ctx, params)

	var toolCalls []ToolCallResult
	var usage *TokenUsage
	isFirst := true

	type blockState struct {
		blockType string
		id        string
		name      string
		inputJSON string
	}
	blocks := make(map[int64]*blockState)

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_start":
			cb := event.ContentBlock
			bs := &blockState{blockType: cb.Type}
			if cb.Type == "tool_use" {
				bs.id = cb.ID
				bs.name = cb.Name
			}
			blocks[event.Index] = bs

		case "content_block_delta":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			switch bs.blockType {
			case "text":
				callback(StreamChunk{
					Content: event.Delta.Text,
					IsFirst: isFirst,
				})
				isFirst = false
			case "tool_use":
				bs.inputJSON += event.Delta.PartialJSON
			}

		case "content_block_stop":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			if bs.blockType == "tool_use" {
				toolCalls = append(toolCalls, ToolCallResult{
					ID:        bs.id,
					Name:      bs.name,
					Arguments: bs.inputJSON,
				})
			}

		case "message_delta":
			if event.Delta.StopReason != "" {
				finishReason := mapStopReason(string(event.Delta.StopReason))
				callback(StreamChunk{
					IsFinal:      true,
					FinishReason: finishReason,
					ToolCalls:    toolCalls,
					Usage:        usage,
				})
			}
			if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 {
				usage = &TokenUsage{
					PromptTokens:     int(event.Usage.InputTokens),
					CompletionTokens: int(event.Usage.OutputTokens),
					TotalTokens:      int(event.Usage.InputTokens + event.Usage.OutputTokens),
				}
			}

		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				usage = &TokenUsage{
					PromptTokens:     int(event.Message.Usage.InputTokens),
					CompletionTokens: int(event.Message.Usage.OutputTokens),
					TotalTokens:      int(event.Message.Usage.InputTokens + event.Message.Usage.OutputTokens),
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return err
	}

	// If we never got a message_delta with stop_reason, send a final chunk.
	if isFirst || len(toolCalls) > 0 {
		callback(StreamChunk{
			IsFinal:      true,
			FinishReason: "stop",
			ToolCalls:    toolCalls,
			Usage:        usage,
		})
	}

	return nil
}

// SendMessageStreamEvents sends a message with granular streaming events.
func (b *AnthropicBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	params := b.buildParams(messages, tools)

	stream := b.client.Messages.NewStreaming(ctx, params)

	var usage *TokenUsage
	var finishReason string

	type blockState struct {
		blockType string
		id        string
		name      string
		inputJSON string
	}
	blocks := make(map[int64]*blockState)

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_start":
			cb := event.ContentBlock
			bs := &blockState{blockType: cb.Type}
			switch cb.Type {
			case "tool_use":
				bs.id = cb.ID
				bs.name = cb.Name
				callback(StreamEvent{
					Type:      EventToolUseStart,
					ToolUseID: cb.ID,
					ToolName:  cb.Name,
				})
			}
			blocks[event.Index] = bs

		case "content_block_delta":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			switch bs.blockType {
			case "text":
				if event.Delta.Text != "" {
					callback(StreamEvent{
						Type:         EventContentDelta,
						ContentDelta: event.Delta.Text,
					})
				}
			case "tool_use":
				if event.Delta.PartialJSON != "" {
					bs.inputJSON += event.Delta.PartialJSON
					callback(StreamEvent{
						Type:       EventToolUseInputDelta,
						ToolUseID:  bs.id,
						InputDelta: event.Delta.PartialJSON,
					})
				}
			}

		case "content_block_stop":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			if bs.blockType == "tool_use" {
				callback(StreamEvent{
					Type:          EventToolUseDone,
					ToolUseID:     bs.id,
					ToolName:      bs.name,
					CompleteInput: bs.inputJSON,
				})
			}

		case "message_delta":
			if event.Delta.StopReason != "" {
				finishReason = mapStopReason(string(event.Delta.StopReason))
			}
			if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 {
				usage = &TokenUsage{
					PromptTokens:     int(event.Usage.InputTokens),
					CompletionTokens: int(event.Usage.OutputTokens),
					TotalTokens:      int(event.Usage.InputTokens + event.Usage.OutputTokens),
				}
			}

		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				usage = &TokenUsage{
					PromptTokens:     int(event.Message.Usage.InputTokens),
					CompletionTokens: int(event.Message.Usage.OutputTokens),
					TotalTokens:      int(event.Message.Usage.InputTokens + event.Message.Usage.OutputTokens),
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return err
	}

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

// convertMessages converts TUI messages to Anthropic format.
// System messages are handled separately via the System parameter.
func (b *AnthropicBackend) convertMessages(messages []tui.ChatMessage) []anthropic.MessageParam {
	var result []anthropic.MessageParam

	for _, msg := range messages {
		// Skip system messages — they are handled via the System parameter.
		if msg.Role == "system" {
			continue
		}

		// Skip empty messages (except tool results which can have empty content).
		if msg.Content == "" && len(msg.ToolCalls) == 0 && msg.Role != "tool" {
			continue
		}

		switch msg.Role {
		case "user":
			var blocks []anthropic.ContentBlockParamUnion

			// Check for image metadata.
			if msg.Metadata != nil {
				if imgType, ok := msg.Metadata["type"].(string); ok && imgType == "image" {
					if b64, ok := msg.Metadata["base64"].(string); ok {
						format, _ := msg.Metadata["format"].(string)
						if format == "" {
							format = "png"
						}
						mediaType := "image/" + format
						blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, b64))
					}
				}
			}

			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}

			if len(blocks) > 0 {
				result = append(result, anthropic.NewUserMessage(blocks...))
			}

		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion

			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}

			// Add tool_use blocks for any tool calls the assistant made.
			for _, tc := range msg.ToolCalls {
				// Parse the arguments JSON to pass as input.
				var input any
				if tc.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
						// Fall back to raw string wrapped in map.
						input = map[string]any{"raw": tc.Arguments}
					}
				} else {
					input = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
			}

			if len(blocks) > 0 {
				result = append(result, anthropic.NewAssistantMessage(blocks...))
			}

		case "tool":
			// Tool result messages.
			isError := false
			if msg.Metadata != nil {
				if errFlag, ok := msg.Metadata["is_error"].(bool); ok {
					isError = errFlag
				}
			}

			toolResultBlock := anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, isError)

			// Wrap in a user message since Anthropic tool results go in user turns.
			result = append(result, anthropic.NewUserMessage(toolResultBlock))

			// If this tool result has image metadata, add it as an image block.
			if msg.Metadata != nil {
				if imgType, ok := msg.Metadata["type"].(string); ok && imgType == "image" {
					if b64, ok := msg.Metadata["base64"].(string); ok {
						format, _ := msg.Metadata["format"].(string)
						if format == "" {
							format = "png"
						}
						mediaType := "image/" + format
						result = append(result, anthropic.NewUserMessage(
							anthropic.NewImageBlockBase64(mediaType, b64),
							anthropic.NewTextBlock(fmt.Sprintf("[Image from tool result: %s]",
								msg.Metadata["filename"])),
						))
					}
				}
			}
		}
	}

	return result
}

// convertTools converts TUI skill definitions to Anthropic tool format.
func (b *AnthropicBackend) convertTools(tools []tui.SkillDefinition) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam

	for _, tool := range tools {
		// Extract properties and required from the parameters map.
		var properties any
		var required []string

		if tool.Parameters != nil {
			if props, ok := tool.Parameters["properties"]; ok {
				properties = props
			}
			if req, ok := tool.Parameters["required"]; ok {
				if reqSlice, ok := req.([]interface{}); ok {
					for _, r := range reqSlice {
						if s, ok := r.(string); ok {
							required = append(required, s)
						}
					}
				}
				// Also handle []string directly.
				if reqSlice, ok := req.([]string); ok {
					required = reqSlice
				}
			}
		}

		toolParam := anthropic.ToolUnionParamOfTool(
			anthropic.ToolInputSchemaParam{
				Properties: properties,
				Required:   required,
			},
			tool.Name,
		)
		toolParam.OfTool.Description = anthropic.String(tool.Description)

		result = append(result, toolParam)
	}

	return result
}

// mapStopReason maps Anthropic stop reasons to the common format used by other backends.
func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return reason
	}
}
