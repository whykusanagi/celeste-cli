package providers

import "testing"

// One rule decides "is this Anthropic" for the header and for the backend
// choice (#372): the URL's host is anthropic.com or a subdomain of it.
func TestIsAnthropicURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"api host", "https://api.anthropic.com", true},
		{"api host trailing slash", "https://api.anthropic.com/", true},
		{"api host with v1", "https://api.anthropic.com/v1", true},
		{"api host with v1 trailing slash", "https://api.anthropic.com/v1/", true},
		{"uppercase host", "https://API.Anthropic.COM/v1", true},
		{"other subdomain", "https://eu.api.anthropic.com", true},
		{"apex", "https://anthropic.com", true},
		{"fqdn trailing dot", "https://api.anthropic.com./v1", true},
		{"with port", "https://api.anthropic.com:443/v1", true},
		{"no scheme", "api.anthropic.com/v1", true},
		{"path on another host", "http://127.0.0.1:18765/anthropic.com", false},
		{"path on another host trailing slash", "http://127.0.0.1:18765/anthropic.com/", false},
		{"lookalike host", "https://notanthropic.com", false},
		{"suffix trick", "https://api.anthropic.com.example.org", false},
		{"query mention", "https://proxy.example.org/v1?u=api.anthropic.com", false},
		{"userinfo trick", "https://api.anthropic.com@evil.example.org/v1", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAnthropicURL(tc.baseURL); got != tc.want {
				t.Fatalf("IsAnthropicURL(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
			if got := DetectProvider(tc.baseURL) == "anthropic"; got != tc.want {
				t.Fatalf("DetectProvider(%q) anthropic = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}

// A local proxy whose path mentions anthropic.com is a local server, not
// Anthropic (#372).
func TestDetectProviderAnthropicPathOnLocalHost(t *testing.T) {
	if got := DetectProvider("http://127.0.0.1:18765/anthropic.com"); got != "local" {
		t.Fatalf("DetectProvider = %q, want local", got)
	}
}
