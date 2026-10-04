package llm

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sashabaranov/go-openai"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// openAIDefaultBaseURL is go-openai's default endpoint, used when the
// config has no base URL.
const openAIDefaultBaseURL = "https://api.openai.com/v1"

// ResponsesBackend talks to OpenAI's Responses API (POST /v1/responses)
// with streaming, tool calls and reasoning items (2.0 W8). Each reply's
// output items become the assistant message's ProviderBlocks and are
// replayed on later turns. The whole history is sent every time
// (store=false; previous_response_id is never used).
type ResponsesBackend struct {
	client         *openai.Client
	config         *Config
	baseURL        string     // effective: Config.BaseURL, or openAIDefaultBaseURL
	mu             sync.Mutex // guards systemPrompt and thinkingConfig
	systemPrompt   string
	thinkingConfig ThinkingConfig
}

// NewResponsesBackend creates a Responses backend for config.
func NewResponsesBackend(config *Config) *ResponsesBackend {
	base := config.BaseURL
	if base == "" {
		base = openAIDefaultBaseURL
	}
	cc := openai.DefaultConfig(config.APIKey)
	cc.BaseURL = base
	cc.HTTPClient = newHTTPClient()
	return &ResponsesBackend{client: openai.NewClientWithConfig(cc), config: config, baseURL: base}
}

func (b *ResponsesBackend) SetSystemPrompt(prompt string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.systemPrompt = prompt
}

func (b *ResponsesBackend) SetThinkingConfig(tc ThinkingConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.thinkingConfig = tc
}

func (b *ResponsesBackend) Close() error { return nil }

// settings reads the prompt and thinking config together; a request may
// be building while the client sets them.
func (b *ResponsesBackend) settings() (string, ThinkingConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.systemPrompt, b.thinkingConfig
}

// providerKey names this backend's blocks: the effective base URL and the
// model it sends (already the served model, #232).
func (b *ResponsesBackend) providerKey() string {
	return ProviderKey(BlocksOpenAIResponses, b.baseURL, b.config.Model)
}

// request builds the Responses request for messages; replayed reports
// whether any message was sent as its recorded items.
func (b *ResponsesBackend) request(messages []tui.ChatMessage, tools []tui.SkillDefinition) (openai.CreateResponseRequest, bool) {
	prompt, thinking := b.settings()
	input, replayed := responsesInput(messages, b.providerKey(), openAIImageLimits(b.baseURL))
	if input == nil {
		input = []json.RawMessage{}
	}
	store := false
	req := openai.CreateResponseRequest{
		Model:        b.config.Model,
		Input:        input,
		Instructions: prompt,
		Store:        &store,
	}
	// store=false keeps nothing server side, so a reasoning model's items
	// must come back encrypted to be replayed (ruling 2). Only reasoning
	// models accept the include; others answer 400.
	if openAIReasoningModel(b.config.Model) {
		req.Include = []openai.ResponseInclude{openai.ResponseIncludeReasoningEncryptedContent}
	}
	if t := responsesTools(tools); len(t) > 0 {
		req.Tools = t
		req.ToolChoice = "auto"
	}
	if effort := openAIEffort(b.config.Model, thinking); effort != "" {
		req.Reasoning = &openai.ResponseReasoning{Effort: effort}
	}
	return req, replayed
}

// turn runs one request and reads its reply. An endpoint known to lack the
// Responses API, or found to lack it now, returns fallback=true and the
// caller makes the same call on Chat Completions (ruling 8). A request
// whose replayed items are refused is resent once with the neutral history
// and returns rejected=true (ruling 10).
func (b *ResponsesBackend) turn(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, emit func(StreamEvent)) (t responsesTurn, rejected, fallback bool, err error) {
	if responsesFellBack(b.baseURL) {
		return t, false, true, nil
	}
	req, replayed := b.request(messages, tools)
	stream, err := b.client.CreateResponseStream(ctx, req)
	if err != nil && replayed && isBlocksRejection(err) {
		tui.LogInfo("openai responses: the endpoint refused replayed items, resending without them: " + err.Error())
		req, _ = b.request(tui.StripProviderBlocks(messages), tools)
		stream, err = b.client.CreateResponseStream(ctx, req)
		rejected = true
	}
	if err != nil && isUnsupportedEndpoint(err) {
		markResponsesFallback(b.baseURL, err)
		return t, false, true, nil
	}
	if err == nil {
		defer stream.Close()
		t, err = readResponses(stream, emit)
	}
	if err != nil && rejected {
		// The resend failed too: the refusal still stands (W8-1 review M4).
		err = &BlocksRejectedError{Err: err}
	}
	return t, rejected, false, err
}

// blocks keeps a reply's output items (ruling 3). A failure to keep them
// never fails the turn.
func (b *ResponsesBackend) blocks(t responsesTurn) *tui.ProviderBlocks {
	pb, err := tui.NewProviderBlocks(b.providerKey(), t.items)
	if err != nil {
		tui.LogInfo("openai responses: output items not kept: " + err.Error())
		return nil
	}
	return pb
}

// SendMessageStreamEvents sends messages and streams granular events.
func (b *ResponsesBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	t, rejected, fallback, err := b.turn(ctx, messages, tools, func(ev StreamEvent) { callback(ev) })
	if fallback {
		return b.fallbackBackend().SendMessageStreamEvents(ctx, messages, tools, callback)
	}
	if err != nil {
		return err
	}
	callback(StreamEvent{Type: EventMessageDone, Usage: t.usage, FinishReason: t.finish,
		ProviderBlocks: b.blocks(t), BlocksRejected: rejected})
	return nil
}

// SendMessageStream sends messages and streams text chunks; the final chunk
// carries the tool calls, usage and blocks.
func (b *ResponsesBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	first := true
	t, rejected, fallback, err := b.turn(ctx, messages, tools, func(ev StreamEvent) {
		if ev.Type == EventContentDelta {
			callback(StreamChunk{Content: ev.ContentDelta, IsFirst: first})
			first = false
		}
	})
	if fallback {
		return b.fallbackBackend().SendMessageStream(ctx, messages, tools, callback)
	}
	if err != nil {
		return err
	}
	callback(StreamChunk{IsFirst: first, IsFinal: true, FinishReason: t.finish, ToolCalls: t.calls,
		Usage: t.usage, ProviderBlocks: b.blocks(t), BlocksRejected: rejected})
	return nil
}

// SendMessageSync sends messages and returns the whole reply.
func (b *ResponsesBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	t, rejected, fallback, err := b.turn(ctx, messages, tools, func(StreamEvent) {})
	if fallback {
		return b.fallbackBackend().SendMessageSync(ctx, messages, tools)
	}
	if err != nil {
		return nil, err
	}
	return &ChatCompletionResult{Content: t.text, ToolCalls: t.calls, FinishReason: t.finish, Usage: t.usage,
		ProviderBlocks: b.blocks(t), BlocksRejected: rejected}, nil
}

// fallbackBackend is the Chat Completions backend for the same endpoint,
// with this backend's prompt and thinking config.
func (b *ResponsesBackend) fallbackBackend() *OpenAIBackend {
	chat := NewOpenAIBackend(b.config)
	prompt, thinking := b.settings()
	chat.SetSystemPrompt(prompt)
	chat.SetThinkingConfig(thinking)
	return chat
}
