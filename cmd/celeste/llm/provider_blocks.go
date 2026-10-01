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
// ToolCalls are sent instead. A backend computes it at request time from
// the effective base URL (after its default is applied) and the model it
// actually sends (after served-model resolution), never from the raw config,
// so the key it captures under equals the key it replays under.
func ProviderKey(format, baseURL, model string) string {
	base := strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/")
	return format + "|" + base + "|" + model
}
