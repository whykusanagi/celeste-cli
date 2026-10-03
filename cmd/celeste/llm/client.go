// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Client wraps LLM backends and provides a unified interface.
// It automatically selects the appropriate backend (OpenAI or Google) based on the provider.
//
// A Client is safe for concurrent use: the TUI switches model while a turn
// streams on the same client. mu guards backend, config, backendType,
// systemPrompt, thinking and toolMode. A request takes one snapshot of
// backend and config per attempt and keeps using that backend even if
// UpdateConfig replaces it meanwhile; no lock is held across a network
// call or a callback.
type Client struct {
	mu           sync.RWMutex
	backend      LLMBackend
	config       *Config
	registry     *tools.Registry
	backendType  BackendType
	systemPrompt string
	// thinking is the last SetThinkingConfig, re-applied when UpdateConfig
	// rebuilds the backend. nil: never set.
	thinking *ThinkingConfig
	// toolMode selects which registered tools GetSkills offers the model.
	// The zero value is tools.ModeChat.
	toolMode tools.RuntimeMode
}

// Config holds LLM client configuration.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	// Backend forces a backend instead of detecting it from BaseURL. Tests
	// use it to reach a fake provider on 127.0.0.1 with a native backend.
	// Empty keeps detection.
	Backend        BackendType
	Timeout        time.Duration
	SimulateTyping bool
	TypingSpeed    int // chars per second

	// Google Cloud authentication (for Gemini/Vertex AI)
	GoogleCredentialsFile string // Path to service account JSON file
	GoogleUseADC          bool   // Use Application Default Credentials

	// Collections (xAI only)
	Collections *config.CollectionsConfig
	XAIFeatures *config.XAIFeaturesConfig
}

// ConfigFrom is the one place a client Config is built from the user's
// configuration, so every path (chat, endpoint switch, single message, MCP,
// agent, summarizer) carries the same fields. A path that needs something
// different overrides it on the result at its call site.
func ConfigFrom(cfg *config.Config) *Config {
	return &Config{
		APIKey:                cfg.APIKey,
		BaseURL:               cfg.BaseURL,
		Model:                 cfg.Model,
		Timeout:               cfg.GetTimeout(),
		SimulateTyping:        cfg.SimulateTyping,
		TypingSpeed:           cfg.TypingSpeed,
		GoogleCredentialsFile: cfg.GoogleCredentialsFile,
		GoogleUseADC:          cfg.GoogleUseADC,
		Collections:           cfg.Collections,
		XAIFeatures:           cfg.XAIFeatures,
	}
}

// NewClientWithBackend builds a Client around an existing backend, skipping
// backend detection. Tests use it to drive the agent loop with a fake.
func NewClientWithBackend(config *Config, registry *tools.Registry, backend LLMBackend) *Client {
	return &Client{backend: backend, config: config, registry: registry}
}

// NewClient creates a new LLM client with automatic backend selection
// (resolveBackendType).
func NewClient(config *Config, registry *tools.Registry) *Client {
	backend, bt := newBackend(config, registry, resolveBackendType(config))
	return &Client{
		backend:     backend,
		config:      config,
		registry:    registry,
		backendType: bt,
	}
}

// resolveBackendType picks the backend for config: Config.Backend when set,
// else detection from the base URL. An OpenAI-compatible endpoint whose
// provider declares Responses support gets the Responses backend (2.0 W8).
func resolveBackendType(config *Config) BackendType {
	if config.Backend != "" {
		return config.Backend
	}
	bt := DetectBackendType(config.BaseURL)
	if bt == BackendTypeOpenAI && usesResponses(config.BaseURL) {
		return BackendTypeOpenAIResponses
	}
	return bt
}

// usesResponses reports whether the provider at baseURL declares Responses
// support. An empty base URL is go-openai's default, OpenAI itself.
func usesResponses(baseURL string) bool {
	if baseURL == "" {
		baseURL = openAIDefaultBaseURL
	}
	caps, ok := providers.GetProvider(providers.DetectProvider(baseURL))
	return ok && caps.SupportsResponses
}

// newBackend builds the backend for bt and returns the type it actually
// built: a native backend that cannot be created falls back to the OpenAI
// SDK backend, as before.
func newBackend(config *Config, registry *tools.Registry, bt BackendType) (LLMBackend, BackendType) {
	switch bt {
	case BackendTypeXAI:
		b, err := NewXAIBackend(config, registry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create xAI backend: %v\nFalling back to OpenAI SDK\n", err)
			return NewOpenAIBackend(config), BackendTypeOpenAI
		}
		tui.LogInfo("Using xAI backend with Collections support")
		return b, bt
	case BackendTypeGoogle:
		b, err := NewGoogleBackend(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create Google backend: %v\nFalling back to OpenAI SDK\n", err)
			return NewOpenAIBackend(config), BackendTypeOpenAI
		}
		return b, bt
	case BackendTypeAnthropic:
		b, err := NewAnthropicBackend(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create Anthropic backend: %v\nFalling back to OpenAI SDK\n", err)
			return NewOpenAIBackend(config), BackendTypeOpenAI
		}
		tui.LogInfo("Using native Anthropic backend with prompt caching")
		return b, bt
	case BackendTypeOpenAIResponses:
		return NewResponsesBackend(config), bt
	default:
		return NewOpenAIBackend(config), BackendTypeOpenAI
	}
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (c *Client) SetSystemPrompt(prompt string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemPrompt = prompt
	if c.backend != nil {
		c.backend.SetSystemPrompt(prompt)
	}
}

// SystemPrompt returns the prompt last set with SetSystemPrompt.
func (c *Client) SystemPrompt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.systemPrompt
}

// SetThinkingConfig configures extended thinking / reasoning effort. It is
// kept and re-applied when UpdateConfig rebuilds the backend.
func (c *Client) SetThinkingConfig(config ThinkingConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thinking = &config
	if c.backend != nil {
		c.backend.SetThinkingConfig(config)
	}
}

// UpdateConfig switches the client to config. The backend is rebuilt
// whenever any field differs, so the next request goes to the new endpoint
// with the new model and the rest (timeout, collections, credentials)
// reaches the backend too; the system prompt and thinking config carry
// over (2.0 W8 ruling 12). A request already in flight finishes on the
// backend it started with, so the old backend is not closed (every
// backend's Close is a no-op). The backend is built outside the lock.
func (c *Client) UpdateConfig(config *Config) {
	bt := resolveBackendType(config)
	c.mu.RLock()
	old, oldBackend, oldType := c.config, c.backend, c.backendType
	c.mu.RUnlock()
	if oldBackend != nil && old != nil && bt == oldType && *old == *config {
		c.mu.Lock()
		// Another UpdateConfig may have installed a different backend since
		// the snapshot; then this config needs its own backend.
		if c.config == old && c.backend == oldBackend {
			c.config = config
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
	backend, built := newBackend(config, c.registry, bt)
	c.mu.Lock()
	defer c.mu.Unlock()
	// Under the lock, so no SetSystemPrompt lands between reading the
	// current backend's state and installing the new one. Lock order is
	// always Client.mu, then a backend's mu.
	if nb, ok := backend.(*AnthropicBackend); ok {
		if ob, ok := c.backend.(*AnthropicBackend); ok {
			nb.inherit(ob)
		}
	}
	c.config = config
	c.backend, c.backendType = backend, built
	if c.systemPrompt != "" {
		c.backend.SetSystemPrompt(c.systemPrompt)
	}
	if c.thinking != nil {
		c.backend.SetThinkingConfig(*c.thinking)
	}
}

// GetConfig returns the current configuration.
func (c *Client) GetConfig() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// snapshot returns the backend and config a request attempt uses. The
// caller uses them without the lock, so a concurrent UpdateConfig affects
// only later attempts.
func (c *Client) snapshot() (LLMBackend, *Config) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.backend, c.config
}

// ServerCompaction reports whether this client's endpoint serves
// server-side compaction (2.0 W8; W1 consumes it). Backends without a
// probe report CompactionUnsupported.
func (c *Client) ServerCompaction(ctx context.Context) CompactionSupport {
	backend, _ := c.snapshot()
	if p, ok := backend.(CompactionProber); ok {
		return p.ProbeCompaction(ctx)
	}
	return CompactionUnsupported
}

// ChatCompletionResult holds the result of a chat completion.
type ChatCompletionResult struct {
	Content      string
	ToolCalls    []ToolCallResult
	FinishReason string
	Error        error
	Usage        *TokenUsage // Token usage from the API response (if available)

	ProviderBlocks *tui.ProviderBlocks // the reply as the provider sent it (2.0 F3); nil from backends that keep none
	BlocksRejected bool                // the provider refused the replayed blocks; the caller strips them (2.0 F3)
}

// ToolCallResult holds a tool call from the LLM.
type ToolCallResult struct {
	ID        string
	Name      string
	Arguments string
	// ArgsError is non-empty when the streamed Arguments could not be assembled
	// into valid JSON (e.g. a dropped stream delta). Empty means no detected
	// corruption — consumers still validate required fields themselves.
	ArgsError string
	// ThoughtSignature carries Gemini 3.x's opaque per-call token through to the
	// assistant message so the next turn can echo it back. Empty elsewhere.
	ThoughtSignature []byte
}

// SendMessageSync sends a message synchronously and returns the result.
// This delegates to the appropriate backend (OpenAI or Google). Every
// attempt sends messages unchanged (2.0 F3: the history is append-only).
func (c *Client) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	var res *ChatCompletionResult
	err := withRetry(ctx, retryOpts{timeout: c.perAttemptTimeout()}, func(reqCtx context.Context) error {
		backend, _ := c.snapshot()
		var e error
		res, e = backend.SendMessageSync(reqCtx, messages, tools)
		return e
	}, func(d time.Duration) { time.Sleep(d) })
	return res, err
}

// perAttemptTimeout is the deadline applied to each individual send attempt.
// Falls back to 60s when the config carries no timeout.
func (c *Client) perAttemptTimeout() time.Duration {
	if _, cfg := c.snapshot(); cfg != nil && cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return 60 * time.Second
}

// StreamCallback is called for each chunk during streaming.
type StreamCallback func(chunk StreamChunk)

// StreamChunk represents a streaming chunk.
// TokenUsage holds token usage information from API response
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// CacheReadTokens and CacheWriteTokens are the parts of PromptTokens
	// served from or written to the prompt cache (Anthropic; OpenAI
	// Responses reports CacheReadTokens only).
	CacheReadTokens  int
	CacheWriteTokens int
	// Estimated: celeste counted these itself, because the provider sent
	// no usage (a stream a steering rule cut short, 2.0 W3).
	Estimated bool
}

type StreamChunk struct {
	Content      string
	IsFirst      bool
	IsFinal      bool
	FinishReason string
	ToolCalls    []ToolCallResult
	Usage        *TokenUsage // Only populated on final chunk with stream_options

	ProviderBlocks *tui.ProviderBlocks // only on the final chunk (2.0 F3)
	BlocksRejected bool                // only on the final chunk: the provider refused the replayed blocks (2.0 F3)
}

// SendMessageStream sends a message with streaming callback.
// This delegates to the appropriate backend (OpenAI or Google).
func (c *Client) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	return withRetry(ctx, retryOpts{timeout: c.perAttemptTimeout()}, func(reqCtx context.Context) error {
		started := false
		wrapped := func(chunk StreamChunk) { started = true; callback(chunk) }
		backend, _ := c.snapshot()
		err := backend.SendMessageStream(reqCtx, messages, tools, wrapped)
		if err != nil && started {
			return fatalErr(err)
		}
		return err
	}, func(d time.Duration) { time.Sleep(d) })
}

// SendMessageStreamEvents sends a message with granular streaming events.
// This delegates to the appropriate backend.
func (c *Client) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	return withRetry(ctx, retryOpts{timeout: c.perAttemptTimeout()}, func(reqCtx context.Context) error {
		started := false
		wrapped := func(ev StreamEvent) { started = true; callback(ev) }
		backend, _ := c.snapshot()
		err := backend.SendMessageStreamEvents(reqCtx, messages, tools, wrapped)
		if err != nil && started {
			return fatalErr(err)
		}
		return err
	}, func(d time.Duration) { time.Sleep(d) })
}

// SetToolMode sets which runtime mode's tools GetSkills offers the model.
// Agent runs call this with tools.ModeAgent; everything else stays on the
// default, tools.ModeChat.
func (c *Client) SetToolMode(mode tools.RuntimeMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolMode = mode
}

// GetSkills returns the tools the model may call in this client's mode.
func (c *Client) GetSkills() []tui.SkillDefinition {
	c.mu.RLock()
	mode := c.toolMode
	c.mu.RUnlock()
	return skillDefinitions(c.registry, mode)
}

// skillDefinitions converts the registry's tools for mode into skill
// definitions. It goes through GetTools rather than GetAll so mode tags and
// discovery-mode hiding apply to what the model is actually sent (#167).
func skillDefinitions(registry *tools.Registry, mode tools.RuntimeMode) []tui.SkillDefinition {
	if registry == nil {
		return nil
	}

	available := registry.GetTools(mode)
	result := make([]tui.SkillDefinition, 0, len(available))
	for _, t := range available {
		var params map[string]interface{}
		if t.Parameters() != nil {
			_ = json.Unmarshal(t.Parameters(), &params)
		}
		result = append(result, tui.SkillDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  params,
		})
	}

	return result
}
