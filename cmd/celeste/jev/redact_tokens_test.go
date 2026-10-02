package jev

import (
	"strings"
	"testing"
)

// A plural key is a token count only when nothing in its name says
// credential: refresh_tokens and its kin are redacted like refresh_token.
func TestRedactPluralCredentialTokenKeys(t *testing.T) {
	for _, in := range []string{
		`{"refresh_tokens":"rt_abcdef123456"}`,
		"access_tokens=abcdef123456",
		"AUTH_TOKENS: abcdef123456",
		`"api_tokens": "abcdef123456"`,
		"session_tokens=abcdef123456",
		"oauthTokens=abcdef123456",
	} {
		if out := Redact(in); strings.Contains(out, "abcdef123456") {
			t.Errorf("leaked: %q -> %q", in, out)
		}
	}
	for _, plain := range []string{
		"max_tokens=1234567",
		"PromptTokens = resp.Usage.PromptTokens",
		`"output_tokens": 123456`,
	} {
		if out := Redact(plain); out != plain {
			t.Errorf("a token count was altered: %q -> %q", plain, out)
		}
	}
}
