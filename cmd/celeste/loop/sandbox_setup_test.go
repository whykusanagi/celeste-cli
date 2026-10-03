package loop

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
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

// A child in another workspace resolves its own policy: the user's
// settings, then that workspace's file, whose loosening needs its own
// trust (a parent's trusted loosening never carries over).
func TestNestedInAnotherWorkspaceResolvesItsOwnPolicy(t *testing.T) {
	home := setupHome(t)
	ws, other := t.TempDir(), t.TempDir()
	write(t, filepath.Join(ws, ".celeste", "config.json"), `{"sandbox":{"enabled":false}}`)
	write(t, filepath.Join(other, ".celeste", "config.json"), `{"sandbox":{"enabled":false,"network":false}}`)
	absWS, _ := filepath.Abs(ws)
	if err := hooks.LoadTrust(home).Approve(hooks.SandboxSource(filepath.Join(absWS, ".celeste", "config.json"), `{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)})
	env, _ := setupWithCfg(t, ModeAgent, cfg, ws)
	if env.SandboxPolicy.Enabled {
		t.Fatalf("parent: the trusted enabled:false applies: %+v", env.SandboxPolicy)
	}
	w := &warnings{}
	child, err := env.Nested(NestedOptions{Workspace: other, Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if !child.SandboxPolicy.Enabled || child.SandboxPolicy.Network {
		t.Fatalf("child: the other workspace's untrusted loosening must not apply, its tightening must: %+v", child.SandboxPolicy)
	}
	if !strings.Contains(w.all(), "celeste hooks trust") {
		t.Fatalf("child warning missing: %s", w.all())
	}
}

// A child in another workspace (an isolated worktree) keeps the user's
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

// Review Focus 2.
func TestRepoSandboxLooseningNeedsTrust(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".celeste", "config.json"), `{"sandbox":{"enabled":false,"writable":["/opt/cache"],"network":false}}`)
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)}) // 2.0: off by default, so the user turned it on
	env, w := setupWithCfg(t, ModeAgent, cfg, ws)              // non-interactive: no approver
	p := env.SandboxPolicy
	if !p.Enabled || slices.Contains(p.Writable, "/opt/cache") {
		t.Fatalf("untrusted loosening applied: %+v", p)
	}
	if p.Network {
		t.Fatal("the repo's network:false tightens and always applies")
	}
	if !strings.Contains(w.all(), "celeste hooks trust") {
		t.Fatalf("warning missing: %s", w.all())
	}
	// Trust it, set up again: the loosening applies.
	body := `{"enabled":false,"network":false,"writable":["/opt/cache"]}`
	if err := hooks.LoadTrust(home).Approve(hooks.SandboxSource(filepath.Join(env.Workspace, ".celeste", "config.json"), body)); err != nil {
		t.Fatal(err)
	}
	env2, w2 := setupWithCfg(t, ModeAgent, cfg, ws)
	if env2.SandboxPolicy.Enabled {
		t.Fatal("a trusted enabled:false applies")
	}
	if strings.Contains(w2.all(), "celeste hooks trust") {
		t.Fatalf("a trusted file warns nothing: %s", w2.all())
	}
	// Any edit asks again.
	write(t, filepath.Join(ws, ".celeste", "config.json"), `{"sandbox":{"enabled":false,"writable":["/"]}}`)
	env3, w3 := setupWithCfg(t, ModeAgent, cfg, ws)
	if !env3.SandboxPolicy.Enabled || !strings.Contains(w3.all(), "changed") {
		t.Fatalf("an edited file is untrusted again: %+v\n%s", env3.SandboxPolicy, w3.all())
	}
}

func TestRepoSandboxTighteningAlwaysApplies(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".celeste", "config.json"), `{"sandbox":{"enabled":true,"network":false}}`)
	env, w := mustSetup(t, ModeAgent, ws)
	if !env.SandboxPolicy.Enabled || env.SandboxPolicy.Network {
		t.Fatalf("policy = %+v", env.SandboxPolicy)
	}
	if strings.Contains(w.all(), "celeste hooks trust") {
		t.Fatalf("tightening needs no trust: %s", w.all())
	}
}

func TestRepoSandboxInteractiveApproval(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".celeste", "config.json"), `{"sandbox":{"enabled":false}}`)
	var asked []hooks.Source
	approve := func(src hooks.Source, _ hooks.TrustStatus) bool {
		asked = append(asked, src)
		return true
	}
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)})
	env, err := Setup(ModeChat, cfg, ws, SetupOptions{Warn: func(string) {}, Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	if env.SandboxPolicy.Enabled {
		t.Fatal("an approved enabled:false applies")
	}
	var src hooks.Source
	for _, s := range asked {
		if s.Kind == hooks.KindRepoSandbox {
			src = s
		}
	}
	if src.Kind != hooks.KindRepoSandbox {
		t.Fatalf("the chat was not asked: %+v", asked)
	}
	if hooks.LoadTrust(home).Status(src) != hooks.Trusted {
		t.Fatal("the approval is stored like any other")
	}
}

func TestRepoSandboxSymlinkIsNeverTrusted(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	real := filepath.Join(t.TempDir(), "c.json")
	write(t, real, `{"sandbox":{"enabled":false}}`)
	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(ws, ".celeste", "config.json")
	if err := os.Symlink(real, p); err != nil {
		t.Skip(err)
	}
	abs, _ := filepath.Abs(p)
	if err := hooks.LoadTrust(home).Approve(hooks.SandboxSource(abs, `{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)})
	env, w := setupWithCfg(t, ModeAgent, cfg, ws)
	if !env.SandboxPolicy.Enabled || !strings.Contains(w.all(), "symlink") {
		t.Fatalf("policy = %+v\n%s", env.SandboxPolicy, w.all())
	}
}

// Review Important 1: an isolated subagent's lane is a linked worktree;
// its git dir and the repository's common dir are outside the lane, and
// git commit there writes to both.
func TestNestedWorktreeLaneCanWriteTheGitDirs(t *testing.T) {
	setupHome(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	gitdir := filepath.Join(repo, ".git", "worktrees", "fire")
	write(t, filepath.Join(lane, ".git"), "gitdir: "+gitdir+"\n")
	write(t, filepath.Join(gitdir, "commondir"), "../..\n")

	cfg := sandboxCfg(&config.Sandbox{Enabled: boolPtr(true)})
	env, _ := setupWithCfg(t, ModeAgent, cfg, repo)
	child, err := env.Nested(NestedOptions{Workspace: lane})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	for _, want := range []string{filepath.Join(repo, ".git"), gitdir} {
		if !slices.Contains(child.SandboxPolicy.Writable, sandbox.Resolve(want)) {
			t.Errorf("lane Writable lacks %s: %v", want, child.SandboxPolicy.Writable)
		}
	}
}

// Review Minor 5: a worktree lane under the parent's workspace whose
// .celeste/config.json has the same sandbox object reuses the parent's
// trust decision; a different object, or a workspace outside the
// parent's, still needs its own.
func TestNestedLaneReusesTheParentsSandboxTrust(t *testing.T) {
	home := setupHome(t)
	repo := t.TempDir()
	const file = `{"sandbox":{"enabled":true,"network":true}}`
	write(t, filepath.Join(repo, ".celeste", "config.json"), file)
	absRepo, _ := filepath.Abs(repo)
	if err := hooks.LoadTrust(home).Approve(hooks.SandboxSource(filepath.Join(absRepo, ".celeste", "config.json"), `{"enabled":true,"network":true}`)); err != nil {
		t.Fatal(err)
	}
	cfg := sandboxCfg(&config.Sandbox{Network: boolPtr(false)})
	env, _ := setupWithCfg(t, ModeAgent, cfg, repo)
	if !env.SandboxPolicy.Network {
		t.Fatalf("parent: the trusted network:true applies: %+v", env.SandboxPolicy)
	}

	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	write(t, filepath.Join(lane, ".celeste", "config.json"), file)
	w := &warnings{}
	child, err := env.Nested(NestedOptions{Workspace: lane, Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if !child.SandboxPolicy.Network || strings.Contains(w.all(), "not trusted") {
		t.Fatalf("lane: the parent's trust carries over for the same object: %+v\n%s", child.SandboxPolicy, w.all())
	}

	other := filepath.Join(repo, ".celeste", "worktrees", "ice")
	write(t, filepath.Join(other, ".celeste", "config.json"), `{"sandbox":{"enabled":true,"network":true,"writable":["/"]}}`)
	w2 := &warnings{}
	child2, err := env.Nested(NestedOptions{Workspace: other, Warn: w2.add})
	if err != nil {
		t.Fatal(err)
	}
	defer child2.Close()
	if child2.SandboxPolicy.Network || !strings.Contains(w2.all(), "not trusted") {
		t.Fatalf("a different object needs its own trust: %+v\n%s", child2.SandboxPolicy, w2.all())
	}

	outside := t.TempDir()
	write(t, filepath.Join(outside, ".celeste", "config.json"), file)
	w3 := &warnings{}
	child3, err := env.Nested(NestedOptions{Workspace: outside, Warn: w3.add})
	if err != nil {
		t.Fatal(err)
	}
	defer child3.Close()
	if child3.SandboxPolicy.Network {
		t.Fatalf("a workspace outside the parent's needs its own trust: %+v", child3.SandboxPolicy)
	}
}
