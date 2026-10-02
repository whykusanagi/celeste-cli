package llm

import "testing"

// "signature" alone is not about thinking: a 400 about a tool parameter
// named signature must not strip the history's replayed blocks.
func TestThinkingRejectionNeedsThinking(t *testing.T) {
	for body, want := range map[string]bool{
		"invalid `signature` in `thinking` block":                                   true,
		"messages.1.content.0.thinking.signature: field required":                   true,
		"messages.3.content.0: invalid signature for redacted_thinking":             true,
		"tools.0.input_schema: property 'signature' must have a type":               false,
		"messages.2.content.1.tool_use.input.signature: string too long (max 1024)": false,
	} {
		if got := isThinkingRejection(body); got != want {
			t.Errorf("isThinkingRejection(%q) = %v, want %v", body, got, want)
		}
	}
}
