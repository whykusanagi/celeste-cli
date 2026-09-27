package llm

import "testing"

// An httptest URL never contains api.anthropic.com, so tests need a way to
// pick the Anthropic backend explicitly (2.0 F1).
func TestConfigBackendOverridesURLDetection(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "m", Backend: BackendTypeAnthropic}, nil)
	if c.backendType != BackendTypeAnthropic {
		t.Fatalf("backendType = %v, want anthropic", c.backendType)
	}
	if _, ok := c.backend.(*AnthropicBackend); !ok {
		t.Fatalf("backend = %T, want *AnthropicBackend", c.backend)
	}
}

func TestConfigBackendEmptyKeepsDetection(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "m"}, nil)
	if c.backendType != BackendTypeOpenAI {
		t.Fatalf("backendType = %v, want openai (detected)", c.backendType)
	}
}
