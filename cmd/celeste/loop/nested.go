package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
)

// Nester builds the Env for a runner started by another run: an *Env built
// by Setup, or a *Parent.
type Nester interface {
	Nested(NestedOptions) (*Env, error)
}

// NestedOptions configures a child Env.
type NestedOptions struct {
	// Workspace is the child's workspace; "" means the parent's. A different
	// one (an isolated worktree) gets its own hooks, project context and
	// code graph.
	Workspace string
	// Warn receives the child's own setup warnings; nil means the parent's
	// sink. A shared hooks runner reports to the sink in the ctx of the run
	// that fired the hook (hooks.WithWarn), else to the parent's.
	Warn func(string)
	// Notice receives the child's timing notices (a git snapshot or
	// code-graph update that ran out of time); nil means Warn when Warn is
	// set, else the parent's.
	Notice func(string)
}

// nestedCodeGraphTimeout bounds how long a child waits for the shared code
// graph to catch up with files changed since its last update. A var so
// tests can shorten it.
var nestedCodeGraphTimeout = 2 * time.Second

// Nested builds the Env for a runner started by another run (a subagent, an
// orchestrator lane), skipping MCP and code-graph startup. The child shares
// the parent's MCP clients (their tools are mirrored into its registry)
// and, in the same workspace, the parent's hooks runner, code graph and
// project context; it re-captures the git state and brings the shared code
// graph up to date first (bounded by nestedCodeGraphTimeout). It always gets
// its own registry (builtins, custom skills; never spawn_agent), its own
// permission checker reloaded from permissions.json (never a parent's
// Trust()), and its own file tracker and snapshots. The shared parts stay
// open until the parent and every child have closed. Only agent-mode Envs
// built by Setup can nest.
func (e *Env) Nested(opts NestedOptions) (*Env, error) {
	if e.Mode != ModeAgent || e.shared == nil || e.nested {
		return nil, errors.New("loop: Nested needs an agent-mode Env built by Setup")
	}
	ws := e.Workspace
	if opts.Workspace != "" {
		abs, err := filepath.Abs(opts.Workspace)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace path: %w", err)
		}
		ws = filepath.Clean(abs)
	}
	e.lifeMu.Lock()
	if e.closed {
		e.lifeMu.Unlock()
		return nil, errors.New("loop: parent environment is closed")
	}
	e.shared.acquire()
	e.lifeMu.Unlock()

	c := &Env{
		Mode:        ModeAgent,
		Workspace:   ws,
		ToolMode:    tools.ModeAgent,
		MCP:         e.MCP,
		opts:        e.opts,
		skipPersona: e.skipPersona,
		home:        e.home,
		shared:      e.shared,
		nested:      true,
	}
	if opts.Warn != nil {
		c.opts.Warn, c.opts.Notice = opts.Warn, opts.Warn
	}
	if opts.Notice != nil {
		c.opts.Notice = opts.Notice
	}
	// The same order as Setup: builtins, skills, permissions, hooks, MCP,
	// discovery mode, then context and the code-graph tools.
	c.Files = checkpoints.NewFileTracker()
	c.Snapshots = checkpoints.NewSnapshotManager(fmt.Sprintf("%s-%d", c.Mode, os.Getpid()))
	c.Registry = tools.NewRegistry()
	builtin.RegisterAll(c.Registry, ws, nil, c.Files, c.Snapshots)
	if err := c.Registry.LoadCustomTools(filepath.Join(c.home, ".celeste", "skills")); err != nil {
		c.warn("custom skills: %v", err)
	}
	// Reloaded, not copied: a rule added since the parent was built applies,
	// and siblings never share a checker's rule slices.
	c.setupPermissions(c.home)
	same := ws == e.Workspace
	if same {
		c.Hooks = e.Hooks
		if e.Hooks != nil {
			if th := e.Hooks.ToolHooks(); th != nil {
				c.Registry.SetHookRunner(th)
			}
		}
	} else {
		c.setupHooks(c.home)
	}
	if e.MCP != nil {
		e.MCP.RegisterInto(c.Registry)
	}
	if c.Registry.Count() > toolDiscoveryThreshold {
		c.Registry.SetDiscoveryMode(true)
	}
	if !same {
		c.ownsIndex = true
		c.setupContext(ws)
		return c, nil
	}
	c.ProjectContext = e.ProjectContext
	c.GitSnapshot = c.captureGit(ws)
	if e.Indexer != nil {
		builtin.RegisterCodeGraphTools(c.Registry, e.Indexer)
		c.Indexer, c.indexOwner = e.Indexer, e.indexOwner
		c.refreshIndex()
	}
	return c, nil
}

// refreshIndex brings the shared code graph up to date with files changed
// since its last update. Only changed files are re-indexed. It waits at most
// nestedCodeGraphTimeout in all: first for an update already running (Setup's
// can outlive its own wait) to finish, then for its own, which it starts only
// if time remains; one that runs longer carries on in the background.
// Updates never overlap. They run under the owner's ctx and WaitGroup, so
// the family's last Close cancels and waits for them. (The child holds a
// reference here, so that Close cannot be running while the WaitGroup is
// added to.)
func (e *Env) refreshIndex() {
	o := e.indexOwner
	if o == nil || o.Indexer == nil {
		return
	}
	timer := time.NewTimer(nestedCodeGraphTimeout)
	defer timer.Stop()
	timedOut := func() {
		e.opts.Notice(fmt.Sprintf("code graph update timed out (%s), continuing with the index as it is", nestedCodeGraphTimeout))
	}
	select {
	case o.indexSem <- struct{}{}:
	case <-o.indexCtx.Done():
		return
	case <-timer.C:
		timedOut()
		return
	}
	done := make(chan error, 1)
	o.indexing.Add(1)
	go func() {
		defer o.indexing.Done()
		defer func() { <-o.indexSem }()
		done <- updateCodeGraph(o.indexCtx, o.Indexer)
	}()
	select {
	case err := <-done:
		if err != nil && o.indexCtx.Err() == nil {
			e.warn("code graph update failed: %v", err)
		}
	case <-timer.C:
		timedOut()
	}
}

// sharedRes counts the Envs using one set of MCP clients and one code graph
// and frees them when the last one is released.
type sharedRes struct {
	mu    sync.Mutex
	refs  int
	close func()
}

func newShared(close func()) *sharedRes { return &sharedRes{refs: 1, close: close} }

func (s *sharedRes) acquire() {
	s.mu.Lock()
	s.refs++
	s.mu.Unlock()
}

func (s *sharedRes) release() {
	s.mu.Lock()
	s.refs--
	last := s.refs == 0
	s.mu.Unlock()
	if last {
		s.close()
	}
}
