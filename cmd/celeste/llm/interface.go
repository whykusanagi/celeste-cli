// Package llm provides the LLM client abstraction for Celeste CLI.
package llm

import (
	"context"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
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

	// SendMessageSync sends a message and returns the complete result.
	// This is useful for non-streaming use cases or testing.
	// Returns the full chat completion result or error.
	SendMessageSync(ctx context.Context, messages []tui.ChatMessage,
		tools []tui.SkillDefinition) (*ChatCompletionResult, error)

	// SetSystemPrompt sets the system prompt (Celeste persona).
	// This configures the LLM's behavior and character.
	SetSystemPrompt(prompt string)

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
)

// DetectBackendType determines which backend to use based on the base URL.
func DetectBackendType(baseURL string) BackendType {
	if isXAIProvider(baseURL) {
		return BackendTypeXAI
	}
	if isGoogleProvider(baseURL) {
		return BackendTypeGoogle
	}
	return BackendTypeOpenAI
}

// isXAIProvider checks if a base URL belongs to xAI (Grok).
func isXAIProvider(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	return contains(baseURL, "api.x.ai") || contains(baseURL, "x.ai/v1")
}

// isGoogleProvider checks if a base URL belongs to Google Cloud.
func isGoogleProvider(baseURL string) bool {
	if baseURL == "" {
		return false
	}

	// Check for Google AI Studio (Gemini API)
	if contains(baseURL, "generativelanguage.googleapis.com") {
		return true
	}

	// Check for Vertex AI
	if contains(baseURL, "aiplatform.googleapis.com") ||
		contains(baseURL, "vertexai") {
		return true
	}

	return false
}

// contains is a simple string contains helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && indexOf(s, substr) >= 0))
}

// indexOf returns the index of substr in s, or -1 if not found
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
