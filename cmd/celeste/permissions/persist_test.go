package permissions

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// A permissions.json that exists but can't be parsed is never rewritten:
// persisting fails, the bytes stay as they were (the user's deny rules are
// still there to fix by hand), and the warn hook hears about it.
func TestPersistLeavesUnreadableFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	truncated := []byte(`{"mode":"default","always_deny":[{"tool_pattern":"bash"`)
	if err := os.WriteFile(path, truncated, 0600); err != nil {
		t.Fatal(err)
	}
	c := NewChecker(DefaultConfig())
	c.SetConfigPath(path)
	var warned error
	c.SetPersistWarn(func(err error) { warned = err })

	if err := c.AddPersistentAllow(Rule{ToolPattern: "write_file"}); err == nil {
		t.Fatal("persist into a truncated file succeeded, want an error")
	}
	if warned == nil {
		t.Fatal("persist failure was not passed to the warn hook")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, truncated) {
		t.Fatalf("file rewritten to %q, want it unchanged", got)
	}
}

// A missing file is created fresh, 0600.
func TestPersistCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "permissions.json")
	c := NewChecker(DefaultConfig())
	c.SetConfigPath(path)
	if err := c.AddPersistentDeny(Rule{ToolPattern: "bash"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if count(cfg.AlwaysDeny, "bash") != 1 {
		t.Fatalf("always_deny = %+v, want bash", cfg.AlwaysDeny)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0600 {
			t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
}

// SaveConfig keeps an existing file's mode.
func TestSaveConfigKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix modes")
	}
	path := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0640 {
		t.Fatalf("mode = %v, want 0640 kept", fi.Mode().Perm())
	}
}

// Saves replace the file atomically: a reader running alongside many saves
// never sees a half-written file or one missing the deny rule.
func TestSaveConfigAtomicUnderConcurrentReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	base := DefaultConfig()
	base.AlwaysDeny = []Rule{{ToolPattern: "bash", Decision: Deny}}
	if err := SaveConfig(path, &base); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	readerErr := make(chan error, 1)
	go func() {
		defer close(readerErr)
		for {
			select {
			case <-done:
				return
			default:
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				readerErr <- err
				return
			}
			if count(cfg.AlwaysDeny, "bash") != 1 {
				readerErr <- fmt.Errorf("read a config without the bash deny rule: %+v", cfg.AlwaysDeny)
				return
			}
		}
	}()

	for i := 0; i < 300; i++ {
		cfg := base
		cfg.AlwaysAllow = nil
		for j := 0; j <= i%40; j++ {
			cfg.AlwaysAllow = append(cfg.AlwaysAllow, Rule{ToolPattern: fmt.Sprintf("tool_%d_%d", i, j), Decision: Allow})
		}
		if err := SaveConfig(path, &cfg); err != nil {
			close(done)
			t.Fatal(err)
		}
	}
	close(done)
	if err := <-readerErr; err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want only permissions.json (temp files left behind?)", len(entries))
	}
}
