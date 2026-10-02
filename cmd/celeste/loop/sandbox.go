package loop

import (
	"log"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
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
// The workspace's git dirs are always writable (sandbox.GitDirs).
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
	extra = append(extra, sandbox.GitDirs(p.Workspace)...)
	p.Writable = sandbox.Normalize(append(sandbox.DefaultWritable(e.home, p.Workspace), e.writablePaths(extra)...))
	e.warnMissingSandbox(p)
	return p
}

// trustRepoSandbox reports whether the workspace config at path, whose
// "sandbox" object is body, may loosen the sandbox: trusted by content
// hash in the hooks trust store, or approved now by the interactive chat
// (and stored). A symlinked file is never trusted.
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
	store := hooks.LoadTrust(e.home)
	if err := store.Err(); err != nil {
		e.warn("sandbox: %v; repository sandbox settings stay untrusted until it is fixed or removed", err)
	}
	src := hooks.SandboxSource(path, body)
	status := store.Status(src)
	if status == hooks.Trusted {
		return true
	}
	if approve := e.approver(); approve != nil && store.Err() == nil && approve(src, status) {
		if err := store.Approve(src); err != nil {
			e.warn("sandbox: %s approved for this session only: %v", strconv.Quote(path), err)
		}
		return true
	}
	if status == hooks.Changed {
		return skip("changed since you approved it")
	}
	return skip("not trusted")
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
