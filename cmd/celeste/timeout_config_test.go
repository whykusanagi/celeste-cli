package main

import (
	"os"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// `config` shows the request timeout and where it came from, so a local
// setup can see the 600 s default took effect (L1).
func TestTimeoutLine(t *testing.T) {
	for _, tc := range []struct {
		cfg  config.Config
		want string
	}{
		{config.Config{BaseURL: "https://api.sakana.ai/v1", Timeout: 60}, "60s without data (default)"},
		{config.Config{BaseURL: "http://127.0.0.1:11434/v1", Timeout: 60}, "600s without data (local default)"},
		{config.Config{BaseURL: "http://127.0.0.1:11434/v1", Timeout: 0}, "600s without data (local default)"},
		{config.Config{BaseURL: "http://127.0.0.1:11434/v1", Timeout: 900}, "900s without data (configured)"},
	} {
		if got := timeoutLine(&tc.cfg); !strings.Contains(got, tc.want) {
			t.Errorf("timeoutLine(%s, %d) = %q, want %q", tc.cfg.BaseURL, tc.cfg.Timeout, got, tc.want)
		}
	}
}

// The timeout error tells the user to run `config --set-timeout`; the flag
// has to exist.
func TestConfigHelpNamesSetTimeout(t *testing.T) {
	if !strings.Contains(usageText, "--set-timeout") {
		t.Error("help does not mention --set-timeout")
	}
}

// The local-model guide documents the timeout behaviour (L1).
func TestLocalGuideDocumentsTimeouts(t *testing.T) {
	data, err := os.ReadFile("../../docs/LLM_PROVIDERS.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	local := doc[strings.Index(doc, "## Local models"):]
	for _, must := range []string{"### Timeouts", "--set-timeout", "600", "30 minutes", "-request-timeout"} {
		if !strings.Contains(local, must) {
			t.Errorf("the local-model section does not mention %q", must)
		}
	}
}

// A negative --set-timeout is refused, not silently ignored.
func TestSetTimeoutError(t *testing.T) {
	for _, tc := range []struct {
		v       int
		given   bool
		wantErr bool
	}{
		{-1, false, false}, // flag absent
		{0, true, false},
		{600, true, false},
		{-1, true, true},
		{-5, true, true},
	} {
		if err := setTimeoutError(tc.v, tc.given); (err != nil) != tc.wantErr {
			t.Errorf("setTimeoutError(%d, %v) = %v, want error %v", tc.v, tc.given, err, tc.wantErr)
		}
	}
}
