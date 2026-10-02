package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A test running under an inherited GIT_DIR (git rebase -x exports it)
// must not touch that repository: Command and Run drop the variable and
// pass the identity with -c, so nothing writes the repo's config.
func TestCommandIgnoresAnInheritedGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bare := filepath.Join(t.TempDir(), "outer.git")
	initBare := exec.Command("git", "init", "--bare", "-q", bare)
	initBare.Env = Env() // this package's own test may run under a leak too
	if out, err := initBare.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	cfg := filepath.Join(bare, "config")
	before, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", bare)
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_INDEX_FILE", filepath.Join(bare, "index"))

	dir := t.TempDir()
	Run(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	Run(t, dir, "add", "a.txt")
	Run(t, dir, "commit", "-q", "-m", "one")

	after, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the inherited GIT_DIR's config changed:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "HEAD")); err != nil {
		t.Fatalf("the repository was not created in the test directory: %v", err)
	}
	if got := Run(t, dir, "log", "-1", "--format=%an <%ae>"); got != Name+" <"+Email+">\n" {
		t.Fatalf("author = %q", got)
	}
	if got := Run(t, dir, "config", "user.name"); got != Name+"\n" {
		t.Fatalf("git config user.name = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "config")); err == nil {
		if b, _ := os.ReadFile(filepath.Join(dir, ".git", "config")); strings.Contains(string(b), Email) {
			t.Fatal("the identity was written into a config file")
		}
	}
}

// Isolate makes the process (and production code it runs) drop the
// inherited variables too.
func TestIsolateDropsInheritedGitVariables(t *testing.T) {
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_WORK_TREE", "/nonexistent")
	Isolate()
	for _, k := range leaked {
		if _, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still set", k)
		}
	}
	if os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" || os.Getenv("GIT_AUTHOR_EMAIL") != Email {
		t.Fatal("Isolate did not set the test identity")
	}
}

// TestIsolateDropsInheritedConfig covers git run by the code under test,
// which inherits the process environment: configuration a parent injected
// (git -c exports GIT_CONFIG_PARAMETERS) must not reach it, and commit
// signing stays off.
func TestIsolateDropsInheritedConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.bare'='true'")
	t.Setenv("GIT_CONFIG", "/nonexistent")
	Isolate()
	for _, k := range []string{"GIT_CONFIG_PARAMETERS", "GIT_CONFIG"} {
		if _, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still set", k)
		}
	}

	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...) // inherits the process env, like the code under test
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if got := git("config", "--get", "core.bare"); got != "false" {
		t.Fatalf("core.bare = %q; an inherited -c reached git", got)
	}
	if got := git("config", "--get", "commit.gpgsign"); got != "false" {
		t.Fatalf("commit.gpgsign = %q", got)
	}
	git("commit", "-q", "--allow-empty", "-m", "one")
}
