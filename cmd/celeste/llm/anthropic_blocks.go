package llm

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// anthropicDefaultBaseURL is the SDK's endpoint when the config has none.
const anthropicDefaultBaseURL = "https://api.anthropic.com"

// replayedContent turns stored blocks into request content blocks that
// marshal to exactly the stored bytes (2.0 W2 ruling 3). A replayed block
// has no cache_control and is never edited.
func replayedContent(raws []json.RawMessage) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, len(raws))
	for i, raw := range raws {
		out[i] = param.Override[anthropic.ContentBlockParamUnion](raw)
	}
	return out
}
