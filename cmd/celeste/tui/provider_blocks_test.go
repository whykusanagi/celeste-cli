package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	keyA = "anthropic-messages|https://api.anthropic.com|claude-a"
	keyB = "openai-responses|https://api.openai.com/v1|gpt-b"
)

func mustBlocks(t *testing.T, provider string, raws ...string) *ProviderBlocks {
	t.Helper()
	var bs []json.RawMessage
	for _, r := range raws {
		bs = append(bs, json.RawMessage(r))
	}
	pb, err := NewProviderBlocks(provider, bs)
	require.NoError(t, err)
	return pb
}

// Spec F3 precedence: the matching backend replays the blocks; any other
// ignores them (and leaves them in place); any edit makes them inert.
func TestReplayBlocksPrecedence(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"type":"thinking","thinking":"t","signature":"S"}`, `{"type":"text","text":"hi"}`)
	msg := AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "hi"}, pb)

	got, ok := ReplayBlocks(msg, keyA)
	require.True(t, ok, "the matching backend replays the blocks")
	assert.Equal(t, pb.Blocks, got)

	_, ok = ReplayBlocks(msg, keyB)
	assert.False(t, ok, "another provider ignores them")
	assert.NotNil(t, msg.ProviderBlocks, "ignoring is not deleting: switching back replays them")
	_, ok = ReplayBlocks(msg, "")
	assert.False(t, ok)

	edited := msg
	edited.Content = "hi!"
	_, ok = ReplayBlocks(edited, keyA)
	assert.False(t, ok, "an edited Content makes the blocks inert")

	withCall := AttachProviderBlocks(ChatMessage{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "echo", Arguments: `{"k":"a"}`}}}, pb)
	_, ok = ReplayBlocks(withCall, keyA)
	require.True(t, ok)
	withCall.ToolCalls = []ToolCallInfo{{ID: "c1", Name: "echo", Arguments: `{"k":"b"}`}}
	_, ok = ReplayBlocks(withCall, keyA)
	assert.False(t, ok, "edited ToolCalls make the blocks inert")
}

func TestAttachProviderBlocks(t *testing.T) {
	msg := ChatMessage{Role: "assistant", Content: "x"}
	assert.Nil(t, AttachProviderBlocks(msg, nil).ProviderBlocks)
	assert.Nil(t, AttachProviderBlocks(msg, &ProviderBlocks{Provider: keyA}).ProviderBlocks, "no blocks: nothing to attach")
	pb := mustBlocks(t, keyA, `{"a":1}`)
	got := AttachProviderBlocks(msg, pb)
	require.NotNil(t, got.ProviderBlocks)
	assert.Equal(t, BlocksDigest("x", nil), got.ProviderBlocks.Digest)
	assert.Empty(t, pb.Digest, "the caller's value is not modified")
	pb.Blocks[0] = json.RawMessage(`{"a":2}`)
	assert.Equal(t, `{"a":1}`, string(got.ProviderBlocks.Blocks[0]), "the block slice is copied")
}

func TestBlocksDigestSeparatesFields(t *testing.T) {
	assert.Len(t, BlocksDigest("x", nil), 32)
	assert.NotEqual(t, BlocksDigest("ab", nil), BlocksDigest("a", []ToolCallInfo{{ID: "b"}}))
	assert.NotEqual(t, BlocksDigest("", []ToolCallInfo{{ID: "a", Name: "b"}}), BlocksDigest("", []ToolCallInfo{{ID: "ab"}}))
	assert.Equal(t,
		BlocksDigest("x", []ToolCallInfo{{ID: "c", ThoughtSignature: []byte("s1")}}),
		BlocksDigest("x", []ToolCallInfo{{ID: "c", ThoughtSignature: []byte("s2")}}),
		"Gemini's ThoughtSignature is not part of the neutral view")
}

func TestCurrentBlocksAndIsEmptyReply(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"type":"compaction","content":"s"}`)
	blocksOnly := AttachProviderBlocks(ChatMessage{Role: "assistant"}, pb)
	assert.NotNil(t, CurrentBlocks(blocksOnly))
	assert.False(t, IsEmptyReply(blocksOnly), "a blocks-only reply is not empty")
	assert.True(t, IsEmptyReply(ChatMessage{Role: "assistant"}))
	assert.False(t, IsEmptyReply(ChatMessage{Role: "user"}))

	stale := blocksOnly
	stale.Content = "x"
	assert.Nil(t, CurrentBlocks(stale))
	stale.Content = ""
	stale.ProviderBlocks = &ProviderBlocks{Provider: keyA, Digest: "nope", Blocks: pb.Blocks}
	assert.True(t, IsEmptyReply(stale), "stale blocks do not make a reply non-empty")
}

func TestStripProviderBlocks(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	msgs := []ChatMessage{{Role: "user", Content: "q"}, AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "a"}, pb)}
	out := StripProviderBlocks(msgs)
	assert.Nil(t, out[1].ProviderBlocks)
	assert.NotNil(t, msgs[1].ProviderBlocks, "copy-on-write")
	plain := []ChatMessage{{Role: "user", Content: "q"}}
	assert.Same(t, &plain[0], &StripProviderBlocks(plain)[0], "nothing to strip: the input comes back")
}

// ReplayBlocks hands out a copy of the slice: a backend that rewrites what it
// sends (W8's ids for store=false, W2's cache_control) works on the copy and
// never changes the recorded message (ruling 14).
func TestReplayBlocksReturnsACopy(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`, `{"b":2}`)
	msg := AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "x"}, pb)
	got, ok := ReplayBlocks(msg, keyA)
	require.True(t, ok)
	got[0] = json.RawMessage(`{"a":9}`)
	again, _ := ReplayBlocks(msg, keyA)
	assert.Equal(t, `{"a":1}`, string(again[0]), "the recorded blocks are unchanged")
}

// Blocks reach a message canonical even when a backend builds the struct
// itself instead of calling NewProviderBlocks: AttachProviderBlocks
// canonicalizes, and attaches nothing when a block is not JSON (ruling 4).
func TestAttachProviderBlocksCanonicalizes(t *testing.T) {
	msg := ChatMessage{Role: "assistant", Content: "x"}
	got := AttachProviderBlocks(msg, &ProviderBlocks{Provider: keyA, Blocks: []json.RawMessage{json.RawMessage("{ \"a\" :\n 1 }")}})
	require.NotNil(t, got.ProviderBlocks)
	assert.Equal(t, `{"a":1}`, string(got.ProviderBlocks.Blocks[0]))
	bad := AttachProviderBlocks(msg, &ProviderBlocks{Provider: keyA, Blocks: []json.RawMessage{json.RawMessage(`{"a":`)}})
	assert.Nil(t, bad.ProviderBlocks, "a block that is not JSON is never attached")
}

// Prune rewrites tool results; an edited message loses its metadata and its
// blocks in the same operation (spec F3 invariant). Others keep theirs.
func TestEditToolResultsClearsBlocks(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	msgs := []ChatMessage{
		AttachProviderBlocks(ChatMessage{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file"}}}, pb),
		AttachProviderBlocks(ChatMessage{Role: "tool", ToolCallID: "c1", Content: "big", Metadata: map[string]any{"type": "image"}}, pb),
		AttachProviderBlocks(ChatMessage{Role: "tool", ToolCallID: "c2", Content: "kept"}, pb),
	}
	out := EditToolResults(msgs, map[string]string{"c1": "[pruned]"})
	assert.Equal(t, "[pruned]", out[1].Content)
	assert.Nil(t, out[1].Metadata)
	assert.Nil(t, out[1].ProviderBlocks, "an edited message loses its blocks")
	assert.NotNil(t, out[0].ProviderBlocks, "the calling turn is not edited")
	assert.NotNil(t, out[2].ProviderBlocks)
	assert.Equal(t, "big", msgs[1].Content, "copy-on-write")
	assert.Same(t, &msgs[0], &EditToolResults(msgs, nil)[0], "no edits: the input comes back")
}
