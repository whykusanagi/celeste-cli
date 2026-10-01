package tui

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// ProviderBlocks is a message's content as one provider returned it (2.0
// F3). See config.ProviderBlocks.
type ProviderBlocks = config.ProviderBlocks

// NewProviderBlocks canonicalizes a reply's blocks for provider (an
// llm.ProviderKey). It returns nil, nil for no blocks.
func NewProviderBlocks(provider string, blocks []json.RawMessage) (*ProviderBlocks, error) {
	return config.NewProviderBlocks(provider, blocks)
}

// BlocksDigest fingerprints a message's provider-neutral view: Content and
// each tool call's ID, Name and Arguments (not Gemini's ThoughtSignature).
// Blocks replay only while their message still has the digest they were
// attached under, so any edit of Content or ToolCalls makes them inert.
func BlocksDigest(content string, calls []ToolCallInfo) string {
	h := sha256.New()
	field := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	field(content)
	for _, c := range calls {
		field(c.ID)
		field(c.Name)
		field(c.Arguments)
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// AttachProviderBlocks returns msg carrying a canonical copy of pb sealed
// to msg's current Content and ToolCalls. A nil pb, an empty provider, no
// blocks or a block that is not JSON leave msg unchanged. Set Content and
// ToolCalls before calling it. Backends should build pb with
// NewProviderBlocks; Attach canonicalizes again so a hand-built value still
// round-trips byte for byte (ruling 4).
func AttachProviderBlocks(msg ChatMessage, pb *ProviderBlocks) ChatMessage {
	if pb == nil {
		return msg
	}
	sealed, err := config.NewProviderBlocks(pb.Provider, pb.Blocks)
	if err != nil || sealed == nil {
		return msg
	}
	sealed.Digest = BlocksDigest(msg.Content, msg.ToolCalls)
	msg.ProviderBlocks = sealed
	return msg
}

// CurrentBlocks returns msg's blocks when they still describe its Content
// and ToolCalls, whatever their provider; nil otherwise. Persistence saves
// only these.
func CurrentBlocks(msg ChatMessage) *ProviderBlocks {
	pb := msg.ProviderBlocks
	if pb == nil || pb.Provider == "" || len(pb.Blocks) == 0 || pb.Digest != BlocksDigest(msg.Content, msg.ToolCalls) {
		return nil
	}
	return pb
}

// ReplayBlocks is the 2.0 F3 precedence rule. When msg's blocks came from
// provider and still match the message, the backend sends them, in order,
// as the message's authoritative content instead of rebuilding it from
// Content and ToolCalls. Otherwise (another provider, an edit, no blocks)
// it returns false and the blocks stay on the message untouched. The slice
// is a copy; the block bytes are shared and read-only (a backend that needs
// to change a block, e.g. to add cache_control, replaces that element).
func ReplayBlocks(msg ChatMessage, provider string) ([]json.RawMessage, bool) {
	pb := CurrentBlocks(msg)
	if pb == nil || provider == "" || pb.Provider != provider {
		return nil, false
	}
	return append([]json.RawMessage(nil), pb.Blocks...), true
}

// IsEmptyReply reports an assistant message that says nothing to any
// provider: no Content, no ToolCalls and no current blocks. A blocks-only
// reply (a server compaction turn) is not empty.
func IsEmptyReply(msg ChatMessage) bool {
	return msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 && CurrentBlocks(msg) == nil
}

// StripProviderBlocks returns msgs without provider blocks, for a prefix
// the provider will no longer accept (W2: a system-prompt change, the
// prefix-mismatch retry). Copy-on-write; msgs itself when none have blocks.
func StripProviderBlocks(msgs []ChatMessage) []ChatMessage {
	out := msgs
	copied := false
	for i := range msgs {
		if msgs[i].ProviderBlocks == nil {
			continue
		}
		if !copied {
			out = append([]ChatMessage(nil), msgs...)
			copied = true
		}
		out[i].ProviderBlocks = nil
	}
	return out
}

// EditToolResults returns a copy of msgs in which the tool results named in
// edits (tool call ID → new content) are replaced: compaction's prune. An
// edited message loses its metadata (inline image data) and its provider
// blocks in the same operation (2.0 F3). No edits: msgs itself.
func EditToolResults(msgs []ChatMessage, edits map[string]string) []ChatMessage {
	if len(edits) == 0 {
		return msgs
	}
	out := append([]ChatMessage(nil), msgs...)
	for i := range out {
		if out[i].Role != "tool" {
			continue
		}
		if c, ok := edits[out[i].ToolCallID]; ok {
			out[i].Content = c
			out[i].Metadata = nil
			out[i].ProviderBlocks = nil
		}
	}
	return out
}
