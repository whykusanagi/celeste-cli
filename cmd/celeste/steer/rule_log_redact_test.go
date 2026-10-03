package steer

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
)

// A rule hit's log line never carries a secret from the matched command:
// the TUI and the server send these lines to log sinks.
func TestRuleHitLogRedactsSecrets(t *testing.T) {
	const key = "ghp_abcdefghijklmnopqrstuvwxyz0123"
	var logged []string
	s := New(Options{Rules: builtins(), RulesMode: "shadow", Logf: func(l string) { logged = append(logged, l) }})
	s.act([]rules.Hit{{Rule: &rules.Rule{Name: "r", Action: rules.Interrupt}, Text: "git push --force https://x:" + key + "@github.com/o/r"}})
	if len(logged) != 1 {
		t.Fatalf("logged %v", logged)
	}
	if strings.Contains(logged[0], key) || strings.Contains(logged[0], "abcdefghijkl") {
		t.Errorf("secret in the log line: %s", logged[0])
	}
}
