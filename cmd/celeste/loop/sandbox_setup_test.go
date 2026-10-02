package loop

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
)

func boolPtr(b bool) *bool { return &b }

func setupWithCfg(t *testing.T, mode Mode, cfg *config.Config, ws string) (*Env, *warnings) {
	t.Helper()
	w := &warnings{}
	env, err := Setup(mode, cfg, ws, SetupOptions{Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return env, w
}

func sandboxCfg(s *config.Sandbox) *config.Config {
	cfg := testCfg()
	cfg.Sandbox = s
	return cfg
}

// Review Focus 5.
func TestNoSandboxWarnsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows logs a line instead (no sandbox is offered there)")
	}
	setupHome(t)
	t.Cleanup(sandbox.SetAvailableForTest("", false))
	resetSandboxWarning()
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)})
	_, w1 := setupWithCfg(t, ModeAgent, cfg, t.TempDir())
	_, w2 := setupWithCfg(t, ModeAgent, cfg, t.TempDir())
	if n := strings.Count(w1.all()+w2.all(), "bash runs without a sandbox"); n != 1 {
		t.Fatalf("warned %d times, want once per process", n)
	}
	if runtime.GOOS == "linux" && !strings.Contains(w1.all(), "install bubblewrap") {
		t.Fatalf("the Linux warning names bubblewrap: %s", w1.all())
	}
}

func TestSandboxOffByDefaultAndNoWarning(t *testing.T) {
	setupHome(t)
	t.Cleanup(sandbox.SetAvailableForTest("", false))
	resetSandboxWarning()
	env, w := mustSetup(t, ModeAgent, t.TempDir())
	if env.SandboxPolicy.Enabled != sandbox.DefaultEnabled {
		t.Fatalf("policy = %+v", env.SandboxPolicy)
	}
	if strings.Contains(w.all(), "sandbox") {
		t.Fatalf("a sandbox that is off warns nothing: %s", w.all())
	}
}

func TestUserSandboxSettingsApply(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	cache := t.TempDir()
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true), Writable: []string{cache, "build-out"}, Network: boolPtr(false)})
	env, _ := setupWithCfg(t, ModeAgent, cfg, ws)
	p := env.SandboxPolicy
	if !p.Enabled || p.Network {
		t.Fatalf("policy = %+v", p)
	}
	for _, want := range []string{ws, cache, filepath.Join(ws, "build-out")} {
		if !slices.Contains(p.Writable, sandbox.Resolve(want)) {
			t.Errorf("Writable lacks %s: %v", want, p.Writable)
		}
	}
	if p.Workspace != sandbox.Resolve(ws) {
		t.Errorf("Workspace = %s", p.Workspace)
	}

	cfg = sandboxCfg(&config.Sandbox{Enabled: boolPtr(false)})
	env, _ = setupWithCfg(t, ModeAgent, cfg, t.TempDir())
	if env.SandboxPolicy.Enabled {
		t.Fatal("the user's enabled:false applies")
	}
}

// A child in another workspace (an isolated worktree) keeps the parent's
// settings with its own workspace writable instead of the parent's.
func TestNestedInheritsTheSandboxPolicy(t *testing.T) {
	setupHome(t)
	ws, other := t.TempDir(), t.TempDir()
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true), Network: boolPtr(false)})
	env, _ := setupWithCfg(t, ModeAgent, cfg, ws)
	child, err := env.Nested(NestedOptions{Workspace: other})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	p := child.SandboxPolicy
	if !p.Enabled || p.Network || p.Workspace != sandbox.Resolve(other) || slices.Contains(p.Writable, sandbox.Resolve(ws)) || !slices.Contains(p.Writable, sandbox.Resolve(other)) {
		t.Fatalf("child policy = %+v", p)
	}
	same, err := env.Nested(NestedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer same.Close()
	if !slices.Equal(same.SandboxPolicy.Writable, env.SandboxPolicy.Writable) {
		t.Fatalf("same-workspace child = %+v", same.SandboxPolicy)
	}
}

func resetSandboxWarning() { sandboxWarnOnce = new(sync.Once) }
