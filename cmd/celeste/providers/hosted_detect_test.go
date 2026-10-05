package providers

import "testing"

// xAI and Google are decided by host, like Anthropic (#372), and the same
// rule serves DetectProvider and the llm backend choice.
func TestHostedProviderURLRules(t *testing.T) {
	cases := []struct {
		url                 string
		xai, gemini, vertex bool
		provider            string
	}{
		{"https://api.x.ai/v1", true, false, false, "grok"},
		{"https://API.X.AI/v1/", true, false, false, "grok"},
		{"api.x.ai/v1", true, false, false, "grok"},
		{"https://x.ai/v1", true, false, false, "grok"},
		{"https://proxy.example.com/x.ai/v1", false, false, false, "unknown"},
		{"https://api.x.ai.example.org/v1", false, false, false, "unknown"},
		{"https://box.ai/v1", false, false, false, "unknown"},
		{"https://generativelanguage.googleapis.com/v1beta", false, true, false, "gemini"},
		{"https://generativelanguage.googleapis.com/v1beta/openai", false, true, false, "gemini"},
		{"https://aiplatform.googleapis.com/v1/projects/p/locations/l", false, false, true, "vertex"},
		{"https://us-central1-aiplatform.googleapis.com/v1", false, false, true, "vertex"},
		{"https://proxy.example.com/generativelanguage.googleapis.com", false, false, false, "unknown"},
		{"https://proxy.example.com/v1?u=aiplatform.googleapis.com", false, false, false, "unknown"},
		{"https://api.anthropic.com", false, false, false, "anthropic"},
		{"", false, false, false, "unknown"},
	}
	for _, tc := range cases {
		if got := IsXAIURL(tc.url); got != tc.xai {
			t.Errorf("IsXAIURL(%q) = %v, want %v", tc.url, got, tc.xai)
		}
		if got := IsGeminiURL(tc.url); got != tc.gemini {
			t.Errorf("IsGeminiURL(%q) = %v, want %v", tc.url, got, tc.gemini)
		}
		if got := IsVertexURL(tc.url); got != tc.vertex {
			t.Errorf("IsVertexURL(%q) = %v, want %v", tc.url, got, tc.vertex)
		}
		if got := IsGoogleURL(tc.url); got != (tc.gemini || tc.vertex) {
			t.Errorf("IsGoogleURL(%q) = %v, want %v", tc.url, got, tc.gemini || tc.vertex)
		}
		if got := DetectProvider(tc.url); got != tc.provider {
			t.Errorf("DetectProvider(%q) = %q, want %q", tc.url, got, tc.provider)
		}
	}
}
