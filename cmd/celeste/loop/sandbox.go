package loop

import (
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
)

// sandboxWarnOnce makes the missing-sandbox notice once per process
// (ruling 8), whichever Env is built first.
var sandboxWarnOnce = new(sync.Once)

// resolveSandbox is bash's OS sandbox policy for this Env (2.0 W4): the
// default, then the user's "sandbox" settings from the loaded config.
func (e *Env) resolveSandbox(cfg *config.Config) sandbox.Policy {
	p := sandbox.Policy{Enabled: sandbox.DefaultEnabled, Network: true}
	var extra []string
	if s := cfg.Sandbox; s != nil {
		if s.Enabled != nil {
			p.Enabled = *s.Enabled
		}
		if s.Network != nil {
			p.Network = *s.Network
		}
		extra = append(extra, s.Writable...)
	}
	p.Workspace = sandbox.Resolve(e.Workspace)
	p.Writable = sandbox.Normalize(append(sandbox.DefaultWritable(e.home, p.Workspace), e.writablePaths(extra)...))
	e.warnMissingSandbox(p)
	return p
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
