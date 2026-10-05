// Package llm provides the LLM client abstraction for Celeste CLI.
package llm

import (
	"context"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// LLMBackend defines the interface all LLM backends must implement.
// This abstraction allows Celeste to support multiple SDK implementations:
// - OpenAI SDK (go-openai) for OpenAI, Grok, Venice, Anthropic, etc.
// - Google GenAI SDK (google.golang.org/genai) for Gemini and Vertex AI
type LLMBackend interface {
	// SendMessageStream sends a message with streaming callback.
	// The callback receives chunks as they arrive from the LLM.
	// Returns error if the request fails.
	SendMessageStream(ctx context.Context, messages []tui.ChatMessage,
		tools []tui.SkillDefinition, callback StreamCallback) error

	// SendMessageStreamEvents sends a message with granular streaming events.
	// Unlike SendMessageStream which batches tool calls into the final chunk,
	// this method delivers tool call information incrementally as it arrives.
	SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage,
		tools []tui.SkillDefinition, callback StreamEventCallback) error

	// SendMessageSync sends a message and returns the complete result.
	// This is useful for non-streaming use cases or testing.
	// Returns the full chat completion result or error.
	SendMessageSync(ctx context.Context, messages []tui.ChatMessage,
		tools []tui.SkillDefinition) (*ChatCompletionResult, error)

	// SetSystemPrompt sets the system prompt (Celeste persona).
	// This configures the LLM's behavior and character.
	SetSystemPrompt(prompt string)

	// SetThinkingConfig configures extended thinking / reasoning effort.
	// Backends that don't support thinking silently ignore the config.
	SetThinkingConfig(config ThinkingConfig)

	// Close cleans up resources (e.g., network connections).
	// Should be called when the backend is no longer needed.
	Close() error
}

// BackendType identifies which SDK implementation is being used.
type BackendType string

const (
	// BackendTypeOpenAI uses the go-openai SDK (OpenAI, Venice, Anthropic, etc.)
	BackendTypeOpenAI BackendType = "openai"

	// BackendTypeGoogle uses the native Google GenAI SDK (Gemini, Vertex AI)
	BackendTypeGoogle BackendType = "google"

	// BackendTypeXAI uses the native xAI SDK with Collections support (Grok models)
	BackendTypeXAI BackendType = "xai"

	// BackendTypeAnthropic uses the native Anthropic SDK (Claude models)
	BackendTypeAnthropic BackendType = "anthropic"

	// BackendTypeOpenAIResponses uses OpenAI's Responses API (2.0 W8): the
	// openai provider, or any provider whose registry entry declares it.
	BackendTypeOpenAIResponses BackendType = "openai-responses"
)

// DetectBackendType determines which backend to use based on the base URL.
// Anthropic, xAI and Google are decided by host, by the same rules
// providers.DetectProvider uses (#372, #377), so the header and the backend
// choice never disagree and a proxy path that names one of those domains
// stays OpenAI-compatible.
func DetectBackendType(baseURL string) BackendType {
	switch {
	case providers.IsAnthropicURL(baseURL):
		return BackendTypeAnthropic
	case providers.IsXAIURL(baseURL):
		return BackendTypeXAI
	case providers.IsGoogleURL(baseURL):
		return BackendTypeGoogle
	}
	return BackendTypeOpenAI
}
