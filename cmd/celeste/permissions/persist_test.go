package permissions

import (
	"path/filepath"
	"testing"
)

// Several checkers persist to one permissions.json (the chat's, and since
// F2c each /agent run's and /orchestrate lane's). A save keeps the rules
// the others saved after this checker loaded the file, instead of writing
// its own stale snapshot over them.
func TestPersistKeepsRulesOtherCheckersSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	chat, lane := NewChecker(DefaultConfig()), NewChecker(DefaultConfig())
	chat.SetConfigPath(path)
	lane.SetConfigPath(path)

	if err := lane.AddPersistentDeny(Rule{ToolPattern: "bash"}); err != nil {
		t.Fatal(err)
	}
	if err := chat.AddPersistentAllow(Rule{ToolPattern: "write_file"}); err != nil {
		t.Fatal(err)
	}
	if err := chat.AddPersistentAllow(Rule{ToolPattern: "write_file"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(cfg.AlwaysDeny, "bash"); n != 1 {
		t.Fatalf("always_deny = %+v, want the lane's bash rule kept", cfg.AlwaysDeny)
	}
	if n := count(cfg.AlwaysAllow, "write_file"); n != 1 {
		t.Fatalf("always_allow = %+v, want write_file once", cfg.AlwaysAllow)
	}
}

func count(rules []Rule, pattern string) int {
	n := 0
	for _, r := range rules {
		if r.ToolPattern == pattern {
			n++
		}
	}
	return n
}
