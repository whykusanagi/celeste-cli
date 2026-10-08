package config

import (
	"strings"
	"testing"
)

// Aikido 806869355 (review): commands.Execute lowercases the command and its
// subcommand, so every casing of set-key sets the key and must be redacted.
func TestRedactSecretCommandIgnoresCase(t *testing.T) {
	for _, line := range []string{
		"/config set-key sk-SECRET",
		"/Config set-key sk-SECRET",
		"/config SET-KEY sk-SECRET",
		"/CONFIG Set-Key sk-SECRET",
		"  /Voice SET-key el-SECRET more",
	} {
		got := RedactSecretCommand(line)
		if strings.Contains(got, "SECRET") || !strings.HasSuffix(got, " ***") {
			t.Errorf("RedactSecretCommand(%q) = %q", line, got)
		}
	}
	if got := RedactSecretCommand("/Config set-model m"); got != "/Config set-model m" {
		t.Errorf("other commands are kept: %q", got)
	}
}
