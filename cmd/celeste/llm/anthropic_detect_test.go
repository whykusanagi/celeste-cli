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
	}
	for _, tc := range cases {
		t.Run(tc.baseURL, func(t *testing.T) {
			native := DetectBackendType(tc.baseURL) == BackendTypeAnthropic
			header := providers.DetectProvider(tc.baseURL) == "anthropic"
			if native != tc.want || header != tc.want {
				t.Fatalf("%q: backend anthropic=%v header anthropic=%v, want both %v", tc.baseURL, native, header, tc.want)
			}
			if got := isAnthropicProvider(tc.baseURL); got != tc.want {
				t.Fatalf("isAnthropicProvider(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}
