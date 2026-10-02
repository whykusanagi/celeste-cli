package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const testRespKey = "openai-responses|http://example.test/v1|gpt-test"

func asStrings(raws []json.RawMessage) []string {
	out := make([]string, len(raws))
	for i, r := range raws {
		out[i] = string(r)
	}
	return out
}

func TestResponsesInputNeutralHistory(t *testing.T) {
	msgs := []tui.ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "read a.go"},
		{Role: "assistant"}, // empty reply: nothing to send
		{Role: "assistant", Content: "reading", ToolCalls: []tui.ToolCallInfo{{ID: "call_a", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		{Role: "tool", ToolCallID: "call_a", Name: "read_file", Content: "package a"},
		{Role: "assistant", Content: "done"},
	}
	got, replayed := responsesInput(msgs, testRespKey)
	assert.False(t, replayed)
	assert.Equal(t, []string{
		`{"type":"message","role":"system","content":"be brief"}`,
		`{"type":"message","role":"user","content":"read a.go"}`,
		`{"type":"message","role":"assistant","content":"reading"}`,
		`{"type":"function_call","call_id":"call_a","name":"read_file","arguments":"{\"path\":\"a.go\"}"}`,
		`{"type":"function_call_output","call_id":"call_a","output":"package a"}`,
		`{"type":"message","role":"assistant","content":"done"}`,
	}, asStrings(got))
}

func TestResponsesInputToolImage(t *testing.T) {
	msgs := []tui.ChatMessage{{Role: "tool", ToolCallID: "c1", Content: "saw it",
		Metadata: map[string]any{"type": "image", "base64": "QUJD", "format": "jpeg", "filename": "shot.jpg"}}}
	got, _ := responsesInput(msgs, testRespKey)
	assert.Equal(t, []string{
		`{"type":"function_call_output","call_id":"c1","output":"saw it"}`,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"[Attached image from tool result: shot.jpg]"},{"type":"input_image","image_url":"data:image/jpeg;base64,QUJD","detail":"auto"}]}`,
	}, asStrings(got))
}

// Ruling 4: replayed items keep their order; only non-reasoning items lose
// their id, and the stored blocks are never modified.
func TestResponsesInputReplaysBlocksWithoutItemIDs(t *testing.T) {
	reasoning := `{"encrypted_content":"enc-1","id":"rs_1","summary":[],"type":"reasoning"}`
	call := `{"arguments":"{}","call_id":"call_a","id":"fc_0","name":"read_file","status":"completed","type":"function_call"}`
	pb, err := tui.NewProviderBlocks(testRespKey, []json.RawMessage{json.RawMessage(reasoning), json.RawMessage(call)})
	require.NoError(t, err)
	asst := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "call_a", Name: "read_file", Arguments: "{}"}}}, pb)
	stored := string(asst.ProviderBlocks.Blocks[1])

	got, replayed := responsesInput([]tui.ChatMessage{{Role: "user", Content: "go"}, asst}, testRespKey)
	assert.True(t, replayed)
	assert.Equal(t, []string{
		`{"type":"message","role":"user","content":"go"}`,
		reasoning,
		`{"arguments":"{}","call_id":"call_a","name":"read_file","status":"completed","type":"function_call"}`,
	}, asStrings(got))
	assert.Equal(t, stored, string(asst.ProviderBlocks.Blocks[1]), "the stored block was modified")
}

// Review Focus 5: another endpoint's or model's items are never sent; the
// neutral view is, and the blocks stay on the message for a switch back.
// Chat Completions never reads blocks at all.
func TestResponsesInputIgnoresOtherProvidersBlocks(t *testing.T) {
	pb, err := tui.NewProviderBlocks("openai-responses|http://example.test/v1|gpt-other", []json.RawMessage{
		json.RawMessage(`{"encrypted_content":"enc-1","id":"rs_1","summary":[],"type":"reasoning"}`),
		json.RawMessage(`{"content":[{"annotations":[],"text":"hi","type":"output_text"}],"id":"msg_1","role":"assistant","status":"completed","type":"message"}`),
	})
	require.NoError(t, err)
	asst := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "hi"}, pb)

	got, replayed := responsesInput([]tui.ChatMessage{asst}, testRespKey)
	assert.False(t, replayed)
	assert.Equal(t, []string{`{"type":"message","role":"assistant","content":"hi"}`}, asStrings(got))
	assert.NotNil(t, asst.ProviderBlocks)

	chat := NewOpenAIBackend(&Config{}).convertMessages([]tui.ChatMessage{asst})
	require.Len(t, chat, 1)
	assert.Equal(t, "hi", chat[0].Content)
}

func TestReplayItemLeavesUnparseableAndIDlessItemsAlone(t *testing.T) {
	for _, raw := range []string{`{"type":"message","role":"assistant","content":"x"}`, `[1,2]`} {
		assert.Equal(t, raw, string(replayItem(json.RawMessage(raw))))
	}
}

func TestResponsesToolsAreFlatAndNotStrict(t *testing.T) {
	tools := responsesTools([]tui.SkillDefinition{
		{Name: "read_file", Description: "Read", Parameters: map[string]any{"type": "object"}},
		{Name: "bad", Parameters: map[string]any{"x": func() {}}},
	})
	require.Len(t, tools, 1, "a tool whose parameters do not marshal is skipped")
	b, err := json.Marshal(tools[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"function","name":"read_file","description":"Read","parameters":{"type":"object"},"strict":false}`, string(b))
}

func TestOpenAIEffort(t *testing.T) {
	on := func(level string) ThinkingConfig { return ThinkingConfig{Enabled: true, Level: level} }
	cases := []struct {
		model string
		tc    ThinkingConfig
		want  string
	}{
		{"gpt-5", on("medium"), "medium"},
		{"o4-mini", on("low"), "low"},
		{"o3", on("max"), "high"},
		{"gpt-4.1-nano", on("high"), ""},
		{"gpt-5", ThinkingConfig{Enabled: false, Level: "high"}, ""},
		{"gpt-5", on("off"), ""},
		{"gpt-5-chat-latest", on("high"), ""},
		{"gpt-4o", on("high"), ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, openAIEffort(c.model, c.tc), "%s %+v", c.model, c.tc)
	}
}

func TestOpenAIReasoningModel(t *testing.T) {
	for model, want := range map[string]bool{
		"o1": true, "o3-mini": true, "o4-mini": true, "gpt-5": true, "GPT-5-mini": true, "gpt-5.1-codex": true,
		"gpt-5-chat-latest": false, "gpt-5-chat": false, "gpt-5.1-chat-latest": false, "GPT-5.2-Chat": false, "o4-mini-chat": false, "gpt-4o": false, "gpt-4o-mini": false, "gpt-4.1": false, "": false,
	} {
		assert.Equal(t, want, openAIReasoningModel(model), model)
	}
}
