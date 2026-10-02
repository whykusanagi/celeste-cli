package jev

import (
	"strings"
	"testing"
)

// A key with "tokens" in it is a count only when its value is a number or
// its name says count and nothing says credential; every other one is
// redacted like a singular token key.
func TestRedactPluralTokenKeys(t *testing.T) {
	for _, in := range []string{
		`{"refresh_tokens":"rt_abcdef123456"}`,
		"access_tokens=abcdef123456",
		"AUTH_TOKENS: abcdef123456",
		`"api_tokens": "abcdef123456"`,
		"session_tokens=abcdef123456",
		"oauthTokens=abcdef123456",
		"github_tokens=ghp_abcdef123456",
		"idTokens: eyJabcdef123456",
		"pushTokens: abcdef123456",
		"deviceTokens: abcdef123456",
		"tokens_secret: abcdef123456",
		"tokens=abcdef123456",
		"userTokens=abcdef123456",
		"max_tokens_key=abcdef123456",
	} {
		if out := Redact(in); strings.Contains(out, "abcdef123456") {
			t.Errorf("leaked: %q -> %q", in, out)
		}
	}
	for _, plain := range []string{
		"max_tokens=1234567",
		"PromptTokens = resp.Usage.PromptTokens",
		"stats.InputTokens = resp.Usage.PromptTokens",
		`"output_tokens": 1234`,
		`"output_tokens": 123456`,
		"total_tokens=resp.Usage.TotalTokens",
		"MatchedTokens:      matched,",
		"deviceTokens: 1234567",
	} {
		if out := Redact(plain); out != plain {
			t.Errorf("a token count was altered: %q -> %q", plain, out)
		}
	}
}
