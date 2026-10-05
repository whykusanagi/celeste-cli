package llm

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
)

// The client's whole prompt is prompts.Prompt.String's bytes for the same
// parts: one join rule (audit D4).
func TestSetSystemPromptPartsJoinsLikePrompt(t *testing.T) {
	for _, p := range []prompts.Prompt{
		{Static: "persona", Dynamic: "rest"},
		{Static: "persona"},
		{Dynamic: "rest"},
		{},
	} {
		c := NewClientWithBackend(&Config{}, nil, nil)
		c.SetSystemPromptParts(p.Static, p.Dynamic)
		if c.systemPrompt != p.String() {
			t.Errorf("parts %q/%q: prompt %q, want %q", p.Static, p.Dynamic, c.systemPrompt, p.String())
		}
	}
}
