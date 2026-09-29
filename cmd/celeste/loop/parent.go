package loop

import (
	"errors"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Parent is the Env the nested runners of one owner share (the subagent
// manager's, or one orchestrator run's). The first Nested call builds it,
// and a later one rebuilds it when ConfigStamp(workspace) has changed, so a
// new deny rule, hook, trust approval, skill or MCP server applies from the
// next runner. A replaced Env closes when the last runner nested under it
// closes.
type Parent struct {
	cfg  *config.Config
	ws   string
	opts SetupOptions

	mu     sync.Mutex
	env    *Env
	stamp  string // ConfigStamp(ws) when env was built
	closed bool
}

// NewParent returns a Parent for workspace. Nothing is built until the
// first Nested call.
func NewParent(cfg *config.Config, workspace string, opts SetupOptions) *Parent {
	return &Parent{cfg: cfg, ws: workspace, opts: opts}
}

// Nested builds a child of the current Env (see Env.Nested), building or
// rebuilding that Env first as needed. It holds the Parent's lock
// throughout, so a rebuild never closes an Env between choosing it and
// nesting under it; concurrent callers wait for each other (Setup takes up
// to 15 s, a child's refresh up to nestedCodeGraphTimeout).
func (p *Parent) Nested(opts NestedOptions) (*Env, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("loop: parent environment is closed")
	}
	// Read under the lock: a stamp read before waiting for it could predate
	// a config change and keep a stale Env.
	stamp := ConfigStamp(p.ws)
	if p.env != nil && p.stamp != stamp {
		// Runners still using it keep its shared parts open until they close.
		p.env.Close()
		p.env = nil
	}
	if p.env == nil {
		env, err := Setup(ModeAgent, p.cfg, p.ws, p.opts)
		if err != nil {
			return nil, err
		}
		p.env, p.stamp = env, stamp
	}
	return p.env.Nested(opts)
}

// Close releases the current Env; runners still using it keep its shared
// parts open until they close. Later Nested calls fail.
func (p *Parent) Close() {
	p.mu.Lock()
	env := p.env
	p.env, p.closed = nil, true
	p.mu.Unlock()
	if env != nil {
		env.Close()
	}
}
