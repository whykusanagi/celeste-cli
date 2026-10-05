package llm

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// The backend choice and the provider shown in the header agree for every
// base URL (#372): a URL is sent to the Anthropic backend exactly when
// DetectProvider calls it anthropic.
func TestBackendChoiceAgreesWithDetectProvider(t *testing.T) {
	cases := []struct {
		baseURL string
		want    bool
	}{
		{"https://api.anthropic.com", true},
		{"https://api.anthropic.com/", true},
		{"https://api.anthropic.com/v1", true},
		{"https://API.ANTHROPIC.COM/v1/", true},
		{"https://eu.api.anthropic.com", true},
		{"https://anthropic.com", true},
		{"http://127.0.0.1:18765/anthropic.com", false},
		{"http://127.0.0.1:18765/anthropic.com/", false},
		{"https://notanthropic.com/v1", false},
		{"https://api.anthropic.com.example.org", false},
		{"https://api.anthropic.com/v1?u=generativelanguage.googleapis.com", true},
		{"https://api.anthropic.com/x.ai/v1", true},
		{"api.anthropic.com/v1?r=http://x", true},
	}
	for _, tc := range cases {
		t.Run(tc.baseURL, func(t *testing.T) {
			native := DetectBackendType(tc.baseURL) == BackendTypeAnthropic
			header := providers.DetectProvider(tc.baseURL) == "anthropic"
			if native != tc.want || header != tc.want {
				t.Fatalf("%q: backend anthropic=%v header anthropic=%v, want both %v", tc.baseURL, native, header, tc.want)
			}
			if got := providers.IsAnthropicURL(tc.baseURL); got != tc.want {
				t.Fatalf("IsAnthropicURL(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}

// xAI and Google follow the same host rule (#377 D5): the backend is
// chosen by the URL's host, exactly as the header's provider is, so a
// proxy path that mentions x.ai or googleapis.com stays OpenAI-compatible.
func TestBackendChoiceAgreesWithDetectProviderXAIGoogle(t *testing.T) {
	cases := []struct {
		baseURL  string
		backend  BackendType
		provider string
	}{
		{"https://api.x.ai/v1", BackendTypeXAI, "grok"},
		{"https://API.X.AI/v1", BackendTypeXAI, "grok"},
		{"https://proxy.example.com/x.ai/v1", BackendTypeOpenAI, "unknown"},
		{"https://generativelanguage.googleapis.com/v1beta", BackendTypeGoogle, "gemini"},
		{"https://us-central1-aiplatform.googleapis.com/v1", BackendTypeGoogle, "vertex"},
		{"https://proxy.example.com/generativelanguage.googleapis.com", BackendTypeOpenAI, "unknown"},
		{"http://127.0.0.1:8080/api.x.ai/v1", BackendTypeOpenAI, "local"},
	}
	for _, tc := range cases {
		if got := DetectBackendType(tc.baseURL); got != tc.backend {
			t.Errorf("DetectBackendType(%q) = %v, want %v", tc.baseURL, got, tc.backend)
		}
		if got := providers.DetectProvider(tc.baseURL); got != tc.provider {
			t.Errorf("DetectProvider(%q) = %q, want %q", tc.baseURL, got, tc.provider)
		}
	}
}
