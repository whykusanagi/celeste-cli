package loop

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
)

// sandboxWarnOnce makes the missing-sandbox notice once per process
// (ruling 8), whichever Env is built first.
var sandboxWarnOnce = new(sync.Once)

// resolveSandbox is bash's OS sandbox policy for this Env (2.0 W4, ruling
// 9): the default, then the user's "sandbox" settings (user, from the
// loaded config), then the workspace's .celeste/config.json. The workspace's
// tightening ("enabled": true, "network": false) always applies; its
// loosening ("enabled": false, "network": true, "writable") only once
// that file's settings are trusted, or the interactive chat approves them
// now. Non-interactive runs skip an untrusted loosening with a warning.
// The workspace's git dirs are writable (sandbox.GitDirs), but never their
// config and hooks (sandbox.GitProtected).
func (e *Env) resolveSandbox(user *config.Sandbox) sandbox.Policy {
	p := sandbox.Policy{Enabled: sandbox.DefaultEnabled, Network: true}
	var extra []string
	if s := user; s != nil {
		if s.Enabled != nil {
			p.Enabled = *s.Enabled
		}
		if s.Network != nil {
			p.Network = *s.Network
		}
		extra = append(extra, s.Writable...)
	}
	repo, path, body, err := config.LoadWorkspaceSandbox(e.Workspace)
	if err != nil {
		e.warn("sandbox: ignoring the workspace's sandbox settings: %v", err)
	}
	if repo != nil {
		if repo.Enabled != nil && *repo.Enabled {
			p.Enabled = true
		}
		if repo.Network != nil && !*repo.Network {
			p.Network = false
		}
		if repo.Loosens() && e.trustRepoSandbox(path, body) {
			if repo.Enabled != nil && !*repo.Enabled {
				p.Enabled = false
			}
			if repo.Network != nil && *repo.Network {
				p.Network = true
			}
			extra = append(extra, repo.Writable...)
		}
	}
	p.Workspace = sandbox.Resolve(e.Workspace)
	// The repository's git dirs: outside a linked worktree (an isolated
	// subagent's lane) or above a subdirectory, and git commit writes there.
	// Never the root or a directory holding the workspace or home, however
	// the repository's metadata got there (GitDirs refuses what does not
	// point back; this is the backstop).
	home := sandbox.Resolve(e.home)
	gitDirs := sandbox.GitDirs(p.Workspace)
	for _, dir := range gitDirs {
		if filepath.Dir(dir) == dir || within(dir, p.Workspace) || (e.home != "" && within(dir, home)) {
			e.warn("sandbox: not making %s writable: it contains the workspace or the home directory", strconv.Quote(dir))
			continue
		}
		extra = append(extra, dir)
	}
	p.Writable = sandbox.Normalize(append(sandbox.DefaultWritable(e.home, p.Workspace), e.writablePaths(extra)...))
	// Their config and hooks stay read-only: celeste and you run git
	// outside the sandbox, and it would run what they name.
	p.ReadOnly = sandbox.GitProtected(gitDirs)
	if p.Enabled && runtime.GOOS == "linux" {
		// bwrap can only bind over a path that exists; git init makes it.
		for _, dir := range gitDirs {
			_ = os.Mkdir(filepath.Join(dir, "hooks"), 0o755)
		}
	}
	e.warnMissingSandbox(p)
	return p
}

// trustRepoSandbox reports whether the workspace config at path, whose
// "sandbox" object is body, may loosen the sandbox: trusted by content
// hash in the hooks trust store, or approved now by the interactive chat
// (and stored), or trusted by the parent of a nested Env under its
// workspace for the same body (e.sandboxTrust). A symlinked file is never
// trusted.
func (e *Env) trustRepoSandbox(path, body string) bool {
	skip := func(why string) bool {
		e.warn("sandbox: ignoring the loosening in %s (%s): a repository's \"sandbox.enabled\": false, \"sandbox.network\": true and \"sandbox.writable\" apply only once trusted; run `celeste hooks trust` to approve them", strconv.Quote(path), why)
		return false
	}
	if e.home == "" {
		return skip("no home directory for the trust store")
	}
	if err := hooks.CheckRepoSandbox(path); err != nil {
		return skip(err.Error())
	}
	if e.sandboxTrust != "" && body == e.sandboxTrust {
		return true // the parent's decision, for a lane under its workspace
	}
	e.sandboxTrust = ""
	trusted := func() bool { e.sandboxTrust = body; return true }
	store := hooks.LoadTrust(e.home)
	if err := store.Err(); err != nil {
		e.warn("sandbox: %v; repository sandbox settings stay untrusted until it is fixed or removed", err)
	}
	src := hooks.SandboxSource(path, body)
	var approve hooks.ApproveFunc
	if store.Err() == nil {
		approve = e.approver()
	}
	run, why, err := hooks.Decide(store, src, approve)
	if err != nil {
		e.warn("sandbox: %v", err)
	}
	if run {
		return trusted()
	}
	return skip(why)
}

// within reports whether path is dir or inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// writablePaths expands "~/" against home and resolves relative entries
// against the workspace.
func (e *Env) writablePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
			continue
		case (p == "~" || strings.HasPrefix(p, "~/")) && e.home != "":
			p = filepath.Join(e.home, strings.TrimPrefix(p, "~"))
		case !filepath.IsAbs(p):
			p = filepath.Join(e.Workspace, p)
		}
		out = append(out, p)
	}
	return out
}

// warnMissingSandbox says once per process that an enabled sandbox is not
// available, so bash runs with the denylist alone (ruling 8). Commands
// never fail for it.
func (e *Env) warnMissingSandbox(p sandbox.Policy) {
	if !p.Enabled {
		return
	}
	if _, ok := sandbox.Available(); ok {
		return
	}
	sandboxWarnOnce.Do(func() {
		switch runtime.GOOS {
		case "linux":
			e.warn("bash runs without a sandbox: install bubblewrap (bwrap) to limit writes to the workspace; the command denylist still applies.")
		case "windows":
			log.Printf("bash runs without an OS sandbox: Windows has none; the command denylist still applies")
		default:
			e.warn("bash runs without a sandbox: the OS sandbox is not available here; the command denylist still applies.")
		}
	})
}
