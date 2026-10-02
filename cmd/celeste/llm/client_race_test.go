package llm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// The TUI switches model (model catalog → applyModel → UpdateConfig) and
// resets the system prompt while a turn is still in flight on the same
// client. Run under -race: requests, model switches, prompt and thinking
// changes on one client must not race.
func TestClientConcurrentRequestsAndReconfigure(t *testing.T) {
	backends := []struct {
		name string
		bt   BackendType
		srv  func(testing.TB, ...fakeprovider.Turn) *fakeprovider.Server
	}{
		{"openai", BackendTypeOpenAI, fakeprovider.NewOpenAI},
		{"responses", BackendTypeOpenAIResponses, fakeprovider.NewOpenAIResponses},
		{"anthropic", BackendTypeAnthropic, fakeprovider.NewAnthropic},
	}
	for _, tc := range backends {
		t.Run(tc.name, func(t *testing.T) {
			resetResponsesFallback()
			t.Cleanup(resetResponsesFallback)
			const n = 20
			srv := tc.srv(t)
			for i := 0; i < 3*n; i++ {
				srv.Push(fakeprovider.Turn{Text: "ok"})
			}
			cfg := func(model string) *Config {
				return &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: model, Timeout: 10 * time.Second, Backend: tc.bt}
			}
			c := NewClient(cfg("model-a"), nil)
			c.SetSystemPrompt("first")

			var wg sync.WaitGroup
			errs := make(chan error, 3*n)
			wg.Add(3)
			go func() {
				defer wg.Done()
				for i := 0; i < n; i++ {
					_, err := c.SendMessageSync(context.Background(), chatMsgs("hi"), nil)
					errs <- err
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < n; i++ {
					errs <- c.SendMessageStream(context.Background(), chatMsgs("hi"), nil, func(StreamChunk) {})
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < n; i++ {
					if i%2 == 0 {
						c.UpdateConfig(cfg("model-b"))
					} else {
						c.UpdateConfig(cfg("model-a"))
					}
					c.SetSystemPrompt("second")
					c.SetThinkingConfig(ThinkingConfig{Enabled: i%2 == 0, Level: "high"})
					_ = c.GetConfig()
					_ = c.ServerCompaction(context.Background())
				}
			}()
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
		})
	}
}

// A request keeps the backend it started on: switching model mid-request
// does not send the rest of that request to the new model, and the next
// request goes to the new one.
func TestUpdateConfigLeavesInFlightRequestOnItsBackend(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "a"}, fakeprovider.Turn{Text: "b"})
	cfg := func(model string) *Config {
		return &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: model, Timeout: 10 * time.Second, Backend: BackendTypeOpenAI}
	}
	c := NewClient(cfg("model-a"), nil)
	switched := false
	err := c.SendMessageStream(context.Background(), chatMsgs("hi"), nil, func(StreamChunk) {
		if !switched {
			switched = true
			c.UpdateConfig(cfg("model-b")) // must not deadlock: no lock is held across the stream
		}
	})
	require.NoError(t, err)
	_, err = c.SendMessageSync(context.Background(), chatMsgs("hi"), nil)
	require.NoError(t, err)
	reqs := srv.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "model-a", reqs[0].Body["model"])
	assert.Equal(t, "model-b", reqs[1].Body["model"])
}

// Minor 4: a config change that keeps endpoint, key and model (timeout,
// collections, credentials) still reaches the backend; it used to keep the
// old Config pointer.
func TestUpdateConfigAppliesNonEndpointChanges(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Timeout: 5 * time.Second, Backend: BackendTypeOpenAIResponses}, nil)
	c.SetSystemPrompt("sys")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	next := &Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Timeout: 42 * time.Second, Backend: BackendTypeOpenAIResponses}
	c.UpdateConfig(next)
	b, ok := c.backend.(*ResponsesBackend)
	require.True(t, ok, "backend = %T", c.backend)
	assert.Same(t, next, b.config)
	assert.Equal(t, "sys", b.systemPrompt)
	assert.Equal(t, ThinkingConfig{Enabled: true, Level: "high"}, b.thinkingConfig)
	assert.Equal(t, 42*time.Second, c.perAttemptTimeout())
}

// Rebuilding an Anthropic backend for the same endpoint and model keeps a
// pending prompt change and a refused binding beta.
func TestUpdateConfigAnthropicRebuildKeepsEndpointState(t *testing.T) {
	cfg := func(model string, timeout time.Duration) *Config {
		return &Config{APIKey: "k", BaseURL: "https://api.anthropic.com", Model: model, Timeout: timeout, Backend: BackendTypeAnthropic}
	}
	c := NewClient(cfg("claude-opus-4-8", time.Minute), nil)
	c.SetSystemPrompt("one")
	c.SetSystemPrompt("two")
	old := c.backend.(*AnthropicBackend)
	old.bindingControls = false

	c.UpdateConfig(cfg("claude-opus-4-8", 2*time.Minute))
	nb := c.backend.(*AnthropicBackend)
	require.NotSame(t, old, nb)
	assert.True(t, nb.promptChanged)
	assert.False(t, nb.bindingControls)
	assert.Equal(t, "two", nb.systemPrompt)

	// Another model on the same endpoint: the prompt change belonged to the
	// old model's blocks, but the endpoint still refuses the beta.
	c.UpdateConfig(cfg("claude-sonnet-4-6", 2*time.Minute))
	other := c.backend.(*AnthropicBackend)
	assert.False(t, other.promptChanged)
	assert.False(t, other.bindingControls, "a model switch forgot the endpoint refused the binding beta")

	// Another endpoint: its own beta state.
	elsewhere := cfg("claude-sonnet-4-6", 2*time.Minute)
	elsewhere.BaseURL = "https://other.example.com"
	c.UpdateConfig(elsewhere)
	fresh := c.backend.(*AnthropicBackend)
	assert.Equal(t, isAnthropicProvider(elsewhere.BaseURL), fresh.bindingControls)
}

// A prompt change made while a request is in flight stays pending: that
// request went out with the old prompt, so the next one must still drop
// replayed blocks.
func TestAnthropicPromptChangeDuringRequestStaysPending(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	b.SetSystemPrompt("one")
	b.SetSystemPrompt("two")
	b.mu.Lock()
	gen := b.promptGen
	b.mu.Unlock()
	b.SetSystemPrompt("three") // lands while the request built with "two" is in flight
	b.clearPromptChanged(gen)
	assert.True(t, b.promptChanged)
	b.clearPromptChanged(b.promptGen)
	assert.False(t, b.promptChanged)
}

// Two concurrent UpdateConfig calls, one a no-op and one a model switch,
// must leave the client's config and backend agreeing: the no-op must not
// install its config next to the other call's backend.
func TestConcurrentUpdateConfigKeepsConfigAndBackendPaired(t *testing.T) {
	cfg := func(model string) *Config {
		return &Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: model, Backend: BackendTypeOpenAIResponses}
	}
	for i := 0; i < 2000; i++ {
		c := NewClient(cfg("a"), nil)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); c.UpdateConfig(cfg("a")) }()
		go func() { defer wg.Done(); c.UpdateConfig(cfg("b")) }()
		wg.Wait()
		b := c.backend.(*ResponsesBackend)
		require.Equal(t, c.config.Model, b.config.Model, "iteration %d: config and backend disagree", i)
	}
}

// The strip decision and the prompt a request sends are read together:
// a request built after a prompt change sends the new prompt without the
// blocks signed over the old one, however the change interleaves with
// open().
func TestAnthropicRequestPairsPromptWithStripDecision(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	b.SetSystemPrompt("one")
	b.SetSystemPrompt("two") // e.g. lands after open() read promptChanged
	_, raw := requestBody(t, prepared(t, b, toolLoop(thinkingTurn(t, b.providerKey()))))
	assert.Contains(t, raw, `"two"`)
	assert.NotContains(t, raw, "sig-1", "blocks signed over the old prompt were sent with the new one")
}
