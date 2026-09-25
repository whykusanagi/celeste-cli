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
//
// The non-thinking default is 32768 (was 8192 through v1.8.x). The old
// 8192 ceiling was too small for the MCP `celeste` tool in chat mode:
// when a sub-tool like code_review returned a multi-KB JSON blob and
// the chat LLM was asked to echo it back, the model hit the output-
// token cap and the MCP response truncated mid-dump. Claude opus and
// sonnet 4.x both support up to 64K output tokens; 32K is a 4x budget
// with no downside for normal chat turns (short responses don't consume
// the budget, only the used tokens are billed).
func (b *AnthropicBackend) maxTokens() int64 {
	// Models that think by default spend output tokens on thinking even when
	// celeste's thinking setting is off, so give them the thinking ceiling.
	switch anthropicThinkingFamily(b.config.Model) {
	case familyAlwaysOn:
		return 65536
	case familyAdaptiveDefaultOn:
		if b.thinkingEnabled() {
			return 65536
		}
	}
	if b.thinkingEnabled() {
		budget := b.thinkingConfig.LevelToBudget()
		if budget > 0 {
			// max_tokens must be > budget_tokens; add generous room for output
			return int64(budget) + 16384
		}
		return 65536 // sensible default when thinking is on
	}
	return 32768 // default for non-thinking requests
}

// buildParams constructs the MessageNewParams shared by sync and streaming requests.
func (b *AnthropicBackend) buildParams(messages []tui.ChatMessage, tools []tui.SkillDefinition) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(b.config.Model),
		MaxTokens: b.maxTokens(),
		Messages:  b.convertMessages(messages),
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
	b.applyThinkingConfig(&params, continuesToolLoop(messages))

	applyCacheBreakpoints(&params)

	return params
}

// messageCacheBreakpoints is how many of the newest messages get a
// cache_control breakpoint. With the system prompt's and the tools' that is
// four, the most a request may carry.
const messageCacheBreakpoints = 2

// applyCacheBreakpoints marks the tool list and the newest messages for
// prompt caching (#174). A breakpoint on the last message writes the whole
// conversation to the cache, so the next request (the same messages plus a
// reply and a new turn) reads it back instead of paying for it again; the
// one on the message before covers a turn that added more blocks than the
// cache lookback reaches. The tools get their own so a system prompt change
// (/persona, /user) keeps them cached.
func applyCacheBreakpoints(params *anthropic.MessageNewParams) {
	if n := len(params.Tools); n > 0 {
		if cc := params.Tools[n-1].GetCacheControl(); cc != nil {
			*cc = anthropic.NewCacheControlEphemeralParam()
		}
	}
	marked := 0
	for i := len(params.Messages) - 1; i >= 0 && marked < messageCacheBreakpoints; i-- {
		blocks := params.Messages[i].Content
		for j := len(blocks) - 1; j >= 0; j-- {
			// Skip blocks that cannot carry cache_control (thinking).
			if cc := blocks[j].GetCacheControl(); cc != nil {
				*cc = anthropic.NewCacheControlEphemeralParam()
				marked++
				break
			}
		}
	}
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

// thinkingFamily groups Claude models by how they take the thinking
// parameter. Current models reject budget_tokens with a 400, and some can't
// have thinking configured at all (#189).
type thinkingFamily int

const (
	// familyBudget: older models (Haiku 4.5, the 4.5 and earlier Sonnet/Opus,
	// 3.x) take {type: "enabled", budget_tokens: N}.
	familyBudget thinkingFamily = iota
	// familyAdaptive: Opus 4.8/4.7/4.6 and Sonnet 4.6 take {type: "adaptive"};
	// omitting the parameter runs without thinking.
	familyAdaptive
	// familyAdaptiveDefaultOn: Opus 5 and Sonnet 5 run adaptive thinking
	// when the parameter is omitted; {type: "disabled"} turns it off.
	familyAdaptiveDefaultOn
	// familyAlwaysOn: Fable, Mythos and Opus 5.5 always think; any explicit
	// thinking configuration other than adaptive is a 400, so it's omitted.
	familyAlwaysOn
)

// anthropicThinkingFamily classifies a model ID. It matches on substrings so
// provider-prefixed IDs (anthropic.claude-opus-5, claude-opus-4-6@...) work.
func anthropicThinkingFamily(model string) thinkingFamily {
	m := strings.ToLower(model)
	has := func(subs ...string) bool {
		for _, sub := range subs {
			if strings.Contains(m, sub) {
				return true
			}
		}
		return false
	}
	switch {
	case has("fable", "mythos", "opus-5-5"):
		return familyAlwaysOn
	case has("opus-5", "sonnet-5"):
		return familyAdaptiveDefaultOn
	case has("opus-4-8", "opus-4-7", "opus-4-6", "sonnet-4-6"):
		return familyAdaptive
	default:
		return familyBudget
	}
}

// thinkingEffort maps celeste's thinking level to the effort parameter used
// with adaptive thinking. Unknown levels leave the model's default.
func thinkingEffort(level string) anthropic.OutputConfigEffort {
	switch level {
	case "low":
		return anthropic.OutputConfigEffortLow
	case "medium":
		return anthropic.OutputConfigEffortMedium
	case "high":
		return anthropic.OutputConfigEffortHigh
	case "max":
		return anthropic.OutputConfigEffortMax
	}
	return ""
}

func (b *AnthropicBackend) thinkingEnabled() bool {
	return b.thinkingConfig.Enabled && b.thinkingConfig.Level != "off"
}

// applyThinkingConfig sets the thinking parameters the model accepts.
//
// continuingToolLoop is true when the request carries tool results back.
// Thinking blocks aren't replayed yet (the shared message type has nowhere
// to keep them, and celeste still edits history between requests), and
// budget-thinking models reject a tool-loop continuation whose assistant
// turn lacks its thinking block, so those turns run without thinking.
// Adaptive models accept the continuation; they reason again from the
// visible history.
func (b *AnthropicBackend) applyThinkingConfig(params *anthropic.MessageNewParams, continuingToolLoop bool) {
	enabled := b.thinkingEnabled()
	switch anthropicThinkingFamily(b.config.Model) {
	case familyAlwaysOn:
		// Never send a thinking parameter; depth is set through effort.
		if enabled {
			params.OutputConfig.Effort = thinkingEffort(b.thinkingConfig.Level)
		}
	case familyAdaptiveDefaultOn:
		if !enabled {
			params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
			return
		}
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
		params.OutputConfig.Effort = thinkingEffort(b.thinkingConfig.Level)
	case familyAdaptive:
		if !enabled {
			return
		}
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
		params.OutputConfig.Effort = thinkingEffort(b.thinkingConfig.Level)
	default:
		if !enabled || continuingToolLoop {
			return
		}
		budget := b.thinkingConfig.LevelToBudget()
		if budget <= 0 {
			budget = 8192 // default budget
		}
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
	}
}

// continuesToolLoop reports whether messages end with tool results, i.e. the
// request continues an assistant turn that called tools.
func continuesToolLoop(messages []tui.ChatMessage) bool {
	return len(messages) > 0 && messages[len(messages)-1].Role == "tool"
}

// usageTracker accumulates usage across a stream. message_start carries the
// input side (including cache reads and writes, which input_tokens excludes);
// message_delta carries cumulative output tokens and usually zero input
// tokens. Replacing the whole struct on message_delta zeroed the prompt
// count, and ignoring the cache fields under-reported it (#189).
type usageTracker struct {
	input, cacheRead, cacheWrite, output int64
	seen                                 bool
}

func (u *usageTracker) start(x anthropic.Usage) {
	u.input, u.cacheRead, u.cacheWrite = x.InputTokens, x.CacheReadInputTokens, x.CacheCreationInputTokens
	u.output = x.OutputTokens
	u.seen = true
}

func (u *usageTracker) delta(x anthropic.MessageDeltaUsage) {
	if x.InputTokens > 0 {
		u.input = x.InputTokens
	}
	if x.CacheReadInputTokens > 0 {
		u.cacheRead = x.CacheReadInputTokens
	}
	if x.CacheCreationInputTokens > 0 {
		u.cacheWrite = x.CacheCreationInputTokens
	}
	if x.OutputTokens > 0 {
		u.output = x.OutputTokens
	}
	u.seen = true
}

// result returns the usage so far, or nil before any usage arrived.
func (u *usageTracker) result() *TokenUsage {
	if !u.seen {
		return nil
	}
	prompt := int(u.input + u.cacheRead + u.cacheWrite)
	return &TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: int(u.output),
		TotalTokens:      prompt + int(u.output),
		CacheReadTokens:  int(u.cacheRead),
		CacheWriteTokens: int(u.cacheWrite),
	}
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
	var tracker usageTracker

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
			tracker.delta(event.Usage)
			result.Usage = tracker.result()

		case "message_start":
			tracker.start(event.Message.Usage)
			result.Usage = tracker.result()
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
	var tracker usageTracker

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
			// Update usage BEFORE sending final callback to avoid stale/nil usage.
			tracker.delta(event.Usage)
			usage = tracker.result()
			if event.Delta.StopReason != "" {
				finishReason := mapStopReason(string(event.Delta.StopReason))
				callback(StreamChunk{
					IsFinal:      true,
					FinishReason: finishReason,
					ToolCalls:    toolCalls,
					Usage:        usage,
				})
			}

		case "message_start":
			tracker.start(event.Message.Usage)
			usage = tracker.result()
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
	var tracker usageTracker

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
			tracker.delta(event.Usage)
			usage = tracker.result()

		case "message_start":
			tracker.start(event.Message.Usage)
			usage = tracker.result()
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
