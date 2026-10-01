package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ProviderBlocks is a message's content exactly as one provider returned it
// (2.0 F3): Anthropic thinking, redacted_thinking, text, tool_use and
// compaction blocks; OpenAI Responses output items. Provider is the key of
// the backend that produced them (llm.ProviderKey). Digest ties them to the
// message's provider-neutral Content and ToolCalls at the moment they were
// attached (tui.AttachProviderBlocks); tui.ReplayBlocks replays them only
// while both still match.
//
// Blocks are canonical JSON: compact and HTML-escaped, the form
// encoding/json itself writes, so a save/load cycle returns the same bytes.
// A ProviderBlocks is never modified after it is attached; an edit replaces
// the message's pointer with nil.
type ProviderBlocks struct {
	Provider string            `json:"provider"`
	Digest   string            `json:"digest,omitempty"`
	Blocks   []json.RawMessage `json:"blocks"`
}

// NewProviderBlocks canonicalizes a reply's blocks. It returns nil, nil for
// no blocks, and an error for an empty provider or a block that is not JSON.
func NewProviderBlocks(provider string, blocks []json.RawMessage) (*ProviderBlocks, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	if provider == "" {
		return nil, errors.New("provider blocks need a provider key")
	}
	out := make([]json.RawMessage, len(blocks))
	for i, b := range blocks {
		c, err := canonicalBlock(b)
		if err != nil {
			return nil, fmt.Errorf("provider block %d: %w", i, err)
		}
		out[i] = c
	}
	return &ProviderBlocks{Provider: provider, Blocks: out}, nil
}

// UnmarshalJSON re-canonicalizes each block, undoing the indentation that
// MarshalIndent (sessions, agent checkpoints) puts inside raw JSON. A value
// that does not decode becomes the zero ProviderBlocks, which is never
// replayed: damaged blocks must not fail a session or checkpoint load.
func (p *ProviderBlocks) UnmarshalJSON(data []byte) error {
	type plain ProviderBlocks
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		*p = ProviderBlocks{}
		return nil
	}
	for i, b := range v.Blocks {
		c, err := canonicalBlock(b)
		if err != nil {
			*p = ProviderBlocks{}
			return nil
		}
		v.Blocks[i] = c
	}
	*p = ProviderBlocks(v)
	return nil
}

func canonicalBlock(b []byte) (json.RawMessage, error) {
	if !json.Valid(b) {
		return nil, errors.New("not valid JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	json.HTMLEscape(&out, compact.Bytes())
	return json.RawMessage(out.Bytes()), nil
}
