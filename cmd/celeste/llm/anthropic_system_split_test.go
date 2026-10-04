package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// wireSystemBlock is one system block as sent.
type wireSystemBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control"`
}

func wireSystem(t *testing.T, raw []byte) []wireSystemBlock {
	t.Helper()
	var blocks []wireSystemBlock
	require.NoError(t, json.Unmarshal(splitWire(t, raw).System, &blocks))
	return blocks
}

// #309: the system prompt goes out as the static persona, carrying a cache
// breakpoint, then the dynamic part with none. A /user change (the dynamic
// part) leaves the persona block's bytes as they were, so the cache keeps
// it, and the request stays within four breakpoints.
func TestAnthropicSystemBreakpointAfterStaticPersona(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "a"}, fakeprovider.Turn{Text: "b"})
	c, _ := newAnthropicTestClient(t, srv, "claude-opus-5")
	persona := "You are the persona.\n\n---\n\nStill the persona."
	c.SetSystemPromptParts(persona, "# User\nYou are talking to Summoner.\n\nCurrent date: 2026-10-04")
	assert.Equal(t, persona+"\n\n# User\nYou are talking to Summoner.\n\nCurrent date: 2026-10-04", c.SystemPrompt())

	history := chatMsgs("hi")
	_, err := c.SendMessageSync(context.Background(), history, listFilesSkill())
	require.NoError(t, err)
	c.SetSystemPromptParts(persona, "# User\nYou are talking to Alice.\n\nCurrent date: 2026-10-04")
	_, err = c.SendMessageSync(context.Background(), history, listFilesSkill())
	require.NoError(t, err)

	reqs := srv.Requests()
	require.Len(t, reqs, 2)
	first, second := wireSystem(t, reqs[0].Raw), wireSystem(t, reqs[1].Raw)
	for i, sys := range [][]wireSystemBlock{first, second} {
		require.Len(t, sys, 2, "request %d", i)
		assert.Equal(t, persona, sys[0].Text, "request %d: the first block is exactly the static persona", i)
		assert.JSONEq(t, `{"type":"ephemeral"}`, string(sys[0].CacheControl), "request %d: the persona block is a breakpoint", i)
		assert.Empty(t, sys[1].CacheControl, "request %d: the dynamic block is not", i)
		assert.LessOrEqual(t, strings.Count(string(reqs[i].Raw), `"cache_control"`), 4, "request %d", i)
	}
	assert.Contains(t, second[1].Text, "Alice")
	p0, p1 := splitWire(t, reqs[0].Raw), splitWire(t, reqs[1].Raw)
	var s0, s1 []json.RawMessage
	require.NoError(t, json.Unmarshal(p0.System, &s0))
	require.NoError(t, json.Unmarshal(p1.System, &s1))
	assert.Equal(t, string(s0[0]), string(s1[0]), "the /user change re-wrote the persona block")
	assert.Equal(t, string(p0.Tools), string(p1.Tools), "the tool block changed")
}

// Four breakpoints at most with a long tool turn: tools, persona, and the
// two newest messages.
func TestAnthropicSplitSystemKeepsFourBreakpoints(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}}
	b.SetSystemPromptParts("persona", "today")
	params := b.buildParams(chatMsgs("a"), listFilesSkill())
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(string(raw), `"cache_control"`), "tools + persona + one message")
	require.Len(t, params.System, 2)
	assert.Equal(t, "persona", params.System[0].Text)
	assert.Equal(t, "today", params.System[1].Text)
}

// A prompt set as one string is one block with a breakpoint; a "---" in it
// is the persona's own text, never a split point.
func TestAnthropicWholePromptIsOneBlock(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}}
	prompt := "Static persona content\n\n---\n\nmore persona"
	b.SetSystemPrompt(prompt)
	params := b.buildParams(chatMsgs("a"), nil)
	require.Len(t, params.System, 1)
	assert.Equal(t, prompt, params.System[0].Text)
	raw, err := json.Marshal(params.System[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"cache_control"`)
}

// An empty part sends one block: the non-empty one, with the breakpoint.
func TestAnthropicSplitSystemWithAnEmptyPart(t *testing.T) {
	for _, tc := range []struct{ static, dynamic, want string }{
		{"persona", "", "persona"},
		{"", "dynamic", "dynamic"},
	} {
		b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}}
		b.SetSystemPromptParts(tc.static, tc.dynamic)
		params := b.buildParams(chatMsgs("a"), nil)
		require.Len(t, params.System, 1, tc.want)
		assert.Equal(t, tc.want, params.System[0].Text)
	}
}

// A dynamic-only change is a prompt change for replayed blocks (their
// signatures cover the whole prompt, ruling 10).
func TestAnthropicDynamicChangeIsAPromptChange(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}}
	b.SetSystemPromptParts("persona", "user: a")
	b.SetSystemPromptParts("persona", "user: a")
	assert.False(t, b.promptChanged)
	b.SetSystemPromptParts("persona", "user: b")
	assert.True(t, b.promptChanged)
}

// An endpoint switch keeps the split: the rebuilt backend gets both parts.
func TestClientUpdateConfigKeepsTheSystemSplit(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "a"})
	cfg := &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "claude-opus-5", Backend: BackendTypeAnthropic}
	c := NewClient(cfg, nil)
	c.SetSystemPromptParts("persona", "today")
	next := *cfg
	next.Model = "claude-sonnet-4-6"
	c.UpdateConfig(&next)
	_, err := c.SendMessageSync(context.Background(), chatMsgs("hi"), nil)
	require.NoError(t, err)
	sys := wireSystem(t, srv.Requests()[0].Raw)
	require.Len(t, sys, 2)
	assert.Equal(t, "persona", sys[0].Text)
	assert.Equal(t, "today", sys[1].Text)
}

// Other backends get the joined prompt.
func TestClientSplitPromptJoinsForOtherBackends(t *testing.T) {
	fb := &fakeSystemBackend{}
	c := NewClientWithBackend(&Config{}, nil, fb)
	c.SetSystemPromptParts("persona", "today")
	assert.Equal(t, "persona\n\ntoday", fb.prompt)
	c.SetSystemPrompt("whole")
	assert.Equal(t, "whole", fb.prompt)
	assert.Equal(t, "whole", c.SystemPrompt())
}

type fakeSystemBackend struct {
	LLMBackend
	prompt string
}

func (f *fakeSystemBackend) SetSystemPrompt(p string) { f.prompt = p }
