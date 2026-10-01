package llm

import "strings"

// Provider-block formats (2.0 F3): the wire format a backend's
// ProviderBlocks are in.
const (
	BlocksAnthropicMessages = "anthropic-messages" // Messages API content blocks (W2, W1)
	BlocksOpenAIResponses   = "openai-responses"   // Responses API output items (W8)
)

// ProviderKey names the backend whose blocks a message carries:
// format|endpoint|model, the endpoint trimmed, lower-cased and without a
// trailing slash. tui.ReplayBlocks replays blocks only to the same key, so
// after an endpoint or model switch the provider-neutral Content and
// ToolCalls are sent instead. A backend computes it from its own config at
// request time.
func ProviderKey(format, baseURL, model string) string {
	base := strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/")
	return format + "|" + base + "|" + model
}
