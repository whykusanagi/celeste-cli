// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
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
	// systemStatic and systemDynamic are systemPrompt's parts when it was
	// set with SetSystemPromptParts; a prompt set whole is all
	// systemDynamic.
	systemStatic  string
	systemDynamic string
	// thinking is the last SetThinkingConfig, re-applied when UpdateConfig
	// rebuilds the backend. nil: never set.
	thinking *ThinkingConfig
	// toolMode selects which registered tools GetSkills offers the model.
	// The zero value is tools.ModeChat.
	toolMode tools.RuntimeMode
	// windowFn, when set, is the context window GetSkills fits the tools
	// to; nil resolves it from config (SetWindowFunc).
	windowFn func() int
	// toolFilter, when set, narrows the registry's tools before the fit
	// (SetToolFilter), so the fit and its notice count only what is sent.
	toolFilter func([]tui.SkillDefinition) []tui.SkillDefinition
	// lastFit and lastWindow are GetSkills' last tool fit, for
	// TakeToolNotice; toolNotices dedupes its notice for this client's
	// session (#310 review: per session, not per process); nil until
	// first used or shared (ShareToolNotices).
	lastFit     compact.ToolFit
	lastWindow  int
	toolNotices *compact.ToolNotices
}

// Config holds LLM client configuration.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	// Backend forces a backend instead of detecting it from BaseURL. Tests
	// use it to reach a fake provider on 127.0.0.1 with a native backend.
	// Empty keeps detection.
	Backend BackendType
	Timeout time.Duration
	// FirstByteTimeout is how long a request may wait for the first byte
	// of the reply, when that is longer than Timeout (the stall timeout
	// between chunks). A local server prefilling a long prompt sends
	// nothing for minutes (#359). Zero, or anything up to Timeout, means
	// Timeout covers the first byte too. FirstByteBudget applies the cap.
	FirstByteTimeout time.Duration

	// Google Cloud authentication (for Gemini/Vertex AI)
	GoogleCredentialsFile string // Path to service account JSON file
	GoogleUseADC          bool   // Use Application Default Credentials

	// ContextLimit is the user's context_limit (0: resolved from the model
	// and endpoint). GetSkills fits the tool schemas to the window (#310).
	ContextLimit int

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
		FirstByteTimeout:      cfg.GetFirstByteTimeout(),
		GoogleCredentialsFile: cfg.GoogleCredentialsFile,
		GoogleUseADC:          cfg.GoogleUseADC,
		ContextLimit:          cfg.ContextLimit,
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

// SetSystemPrompt sets the system prompt (Celeste persona) as one piece.
func (c *Client) SetSystemPrompt(prompt string) {
	c.SetSystemPromptParts("", prompt)
}

// systemPromptPartsSetter is a backend that caches the static part of the
// system prompt apart from the dynamic rest (Anthropic, #309).
type systemPromptPartsSetter interface {
	SetSystemPromptParts(static, dynamic string)
}

// SetSystemPromptParts sets the system prompt from prompts.Compose's
// parts: the byte-stable persona (Prompt.Static) and the rest
// (Prompt.Dynamic). A backend that caches the persona on its own gets the
// parts; the others get the whole prompt, prompts.Prompt.String.
func (c *Client) SetSystemPromptParts(static, dynamic string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemPrompt = prompts.Prompt{Static: static, Dynamic: dynamic}.String()
	c.systemStatic, c.systemDynamic = static, dynamic
	c.applySystemPromptLocked()
}

// applySystemPromptLocked hands the prompt to the backend. c.mu is held.
func (c *Client) applySystemPromptLocked() {
	if c.backend == nil {
		return
	}
	if ps, ok := c.backend.(systemPromptPartsSetter); ok {
		ps.SetSystemPromptParts(c.systemStatic, c.systemDynamic)
		return
	}
	c.backend.SetSystemPrompt(c.systemPrompt)
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
		c.applySystemPromptLocked()
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
	err := withRetry(ctx, c.attemptOpts(), func(reqCtx context.Context) error {
		backend, _ := c.snapshot()
		var e error
		res, e = backend.SendMessageSync(reqCtx, messages, tools)
		return e
	}, func(d time.Duration) { time.Sleep(d) })
	return res, err
}

// attemptOpts are the deadlines for each send attempt: the configured
// timeout is the stall timeout (nothing received for that long ends the
// attempt), and MaxRequestDuration caps an attempt that keeps streaming.
func (c *Client) attemptOpts() retryOpts {
	_, cfg := c.snapshot()
	return retryOpts{stall: cfg.StallTimeout(), firstByte: cfg.FirstByteBudget(), timeout: cfg.RequestCap()}
}

// StallTimeout is the stall timeout of requests made with c: c.Timeout, or
// 60 s when c (which may be nil) carries none.
func (c *Config) StallTimeout() time.Duration {
	if c != nil && c.Timeout > 0 {
		return c.Timeout
	}
	return 60 * time.Second
}

// FirstByteBudget is how long a request made with c may wait for the first
// byte of the reply: FirstByteTimeout when it is longer than the stall
// timeout, capped by RequestCap; otherwise the stall timeout.
func (c *Config) FirstByteBudget() time.Duration {
	stall := c.StallTimeout()
	if c == nil || c.FirstByteTimeout <= stall {
		return stall
	}
	return min(c.FirstByteTimeout, c.RequestCap())
}

// RequestCap bounds one request made with c however steadily it streams:
// MaxRequestDuration of the stall timeout. Work that is one request to the
// model, such as a compaction summary, is bounded by it too (#345).
func (c *Config) RequestCap() time.Duration {
	return MaxRequestDuration(c.StallTimeout())
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
	// served from or written to the prompt cache (Anthropic; OpenAI, xAI
	// and Google report CacheReadTokens only). CacheWrite1hTokens is the
	// part of CacheWriteTokens written with a 1-hour lifetime (Anthropic),
	// which is priced higher than a 5-minute write (#312).
	CacheReadTokens    int
	CacheWriteTokens   int
	CacheWrite1hTokens int
	// Estimated: celeste counted these itself, because the provider sent
	// no usage (a stream a steering rule cut short, 2.0 W3).
	Estimated bool
}

// CostUsage is u as costs.CostOf prices it: the prompt with its cache
// reads and writes (#312).
func (u *TokenUsage) CostUsage() costs.Usage {
	return costs.Usage{
		Input: u.PromptTokens, Output: u.CompletionTokens,
		CacheRead: u.CacheReadTokens, CacheWrite: u.CacheWriteTokens, CacheWrite1h: u.CacheWrite1hTokens,
	}
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
	return withRetry(ctx, c.attemptOpts(), func(reqCtx context.Context) error {
		started := false
		wrapped := func(chunk StreamChunk) { started = true; touchStall(reqCtx); callback(chunk) }
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
	return withRetry(ctx, c.attemptOpts(), func(reqCtx context.Context) error {
		// Every event, reasoning included, is activity on the stall watch:
		// a local model that thinks for minutes before replying is alive.
		// Reasoning alone does not start the reply, though: a drop while a
		// slow local model is still thinking is retried (L4). The repeated
		// thinking only feeds a status-bar count.
		started := false
		wrapped := func(ev StreamEvent) {
			touchStall(reqCtx)
			if ev.Type != EventThinkingDelta {
				started = true
			}
			callback(ev)
		}
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

// SetWindowFunc makes GetSkills fit the tools to the window fn returns
// (the chat follows its live model and context_limit). fn is called
// without the client's lock held, so it may call the client.
func (c *Client) SetWindowFunc(fn func() int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.windowFn = fn
}

// SetToolFilter makes GetSkills narrow the tools with fn before fitting
// them to the window (the chat drops submit_plan outside plan mode, and
// every write tool in it), so the fit spends its budget only on tools that
// are sent and TakeToolNotice counts those (K2). fn is called without the
// client's lock held and must not modify its argument.
func (c *Client) SetToolFilter(fn func([]tui.SkillDefinition) []tui.SkillDefinition) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolFilter = fn
}

// GetSkills returns the tools the model may call in this client's mode,
// fitted to the context window next to the system prompt
// (compact.FitTools): on a small window a core set with short
// descriptions, plus the tools find_tools activated (the newest first
// while they fit). When that reduces the set, TakeToolNotice says so once.
func (c *Client) GetSkills() []tui.SkillDefinition {
	c.mu.RLock()
	mode, cfg, system, windowFn, filter := c.toolMode, c.config, c.systemPrompt, c.windowFn, c.toolFilter
	c.mu.RUnlock()
	defs := skillDefinitions(c.registry, mode)
	if filter != nil {
		defs = filter(defs)
	}
	window := 0
	switch {
	case windowFn != nil:
		window = windowFn()
	case cfg != nil:
		window, _ = config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit, cfg.APIKey)
	}
	var pinned []string
	if c.registry != nil {
		pinned = c.registry.ActivatedNames()
	}
	fit := compact.FitTools(defs, window, ctxmgr.EstimateTokens(system), pinned)
	c.mu.Lock()
	c.lastFit, c.lastWindow = fit, window
	c.mu.Unlock()
	if fit.Defs == nil {
		// Empty, never nil: the loop runs only the tools offered, and nil
		// would place no restriction (loop.LLM).
		return []tui.SkillDefinition{}
	}
	return fit.Defs
}

// TakeToolNotice returns the notice for GetSkills' last fit, once per fit
// for this client ("" when it was not reduced or was already told). The
// caller shows it, as it shows the persona guard's.
func (c *Client) TakeToolNotice() string {
	c.mu.Lock()
	fit, window := c.lastFit, c.lastWindow
	if c.toolNotices == nil {
		c.toolNotices = new(compact.ToolNotices)
	}
	seen := c.toolNotices
	c.mu.Unlock()
	return seen.Notice(fit, window)
}

// ShareToolNotices makes TakeToolNotice dedupe through n, shared with other
// clients: a server whose every request has its own client tells its log
// once per fit, not once per request.
func (c *Client) ShareToolNotices(n *compact.ToolNotices) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolNotices = n
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
