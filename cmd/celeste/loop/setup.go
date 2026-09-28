package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// Mode is the surface a run belongs to.
type Mode int

const (
	ModeChat    Mode = iota // the TUI
	ModeAgent               // celeste agent, subagents, orchestrator, /agent, MCP mode:"agent"
	ModeMCPChat             // MCP celeste mode:"chat"
)

func (m Mode) String() string {
	switch m {
	case ModeAgent:
		return "agent"
	case ModeMCPChat:
		return "mcp"
	default:
		return "tui"
	}
}

// toolDiscoveryThreshold turns on discovery mode (MCP tools hidden until
// find_tools activates them) once the registry is this big.
const toolDiscoveryThreshold = 40

// Env is everything a mode needs around the loop.
type Env struct {
	Mode           Mode
	Workspace      string
	Registry       *tools.Registry
	Checker        *permissions.Checker
	ToolMode       tools.RuntimeMode
	MCP            *mcp.Manager
	Indexer        *codegraph.Indexer
	Hooks          *hooks.Runner // tool hooks run in Registry; nil when loading failed
	Files          *checkpoints.FileTracker
	Snapshots      *checkpoints.SnapshotManager
	ProjectContext string // grimoire, project memories, code-graph summary
	GitSnapshot    string

	opts        SetupOptions
	skipPersona bool
	permConfig  permissions.PermissionConfig
	indexing    sync.WaitGroup     // a code-graph update that outlived its timeout
	indexCancel context.CancelFunc // stops that update; nil until setupCodeGraph runs
	closeOnce   sync.Once
}

// SetupOptions carries what only the adopter knows.
type SetupOptions struct {
	SessionID string       // hooks' session_id; "" = "<mode>-<pid>"
	Warn      func(string) // setup and hook warnings; nil = stderr
}

// Setup builds the registry, MCP clients, custom skills, permission checker,
// memories, grimoire, git snapshot, code graph and hooks for mode (2.0 F2).
// The caller owns Close.
func Setup(mode Mode, cfg *config.Config, workspace string, opts SetupOptions) (*Env, error) {
	if cfg == nil {
		return nil, fmt.Errorf("loop.Setup: config is required")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}
	ws := filepath.Clean(abs)
	home, _ := os.UserHomeDir()

	if opts.Warn == nil {
		opts.Warn = func(s string) { fmt.Fprintln(os.Stderr, "Warning: "+s) }
	}
	if opts.SessionID == "" {
		opts.SessionID = fmt.Sprintf("%s-%d", mode, os.Getpid())
	}
	env := &Env{Mode: mode, Workspace: ws, ToolMode: tools.ModeChat, opts: opts, skipPersona: cfg.SkipPersonaPrompt}
	if mode == ModeAgent {
		env.ToolMode = tools.ModeAgent
	}
	env.Files = checkpoints.NewFileTracker()
	env.Snapshots = checkpoints.NewSnapshotManager(fmt.Sprintf("%s-%d", mode, os.Getpid()))
	env.Registry = tools.NewRegistry()
	builtin.RegisterAll(env.Registry, ws, nil, env.Files, env.Snapshots)
	if err := env.Registry.LoadCustomTools(filepath.Join(home, ".celeste", "skills")); err != nil {
		env.warn("custom skills: %v", err)
	}
	env.setupPermissions(home)
	env.setupHooks(home)
	env.setupMCP(ws, home)
	if env.Registry.Count() > toolDiscoveryThreshold {
		env.Registry.SetDiscoveryMode(true)
	}
	env.setupContext(ws)
	return env, nil
}

func (e *Env) warn(format string, args ...any) {
	e.opts.Warn(fmt.Sprintf(format, args...))
}

func (e *Env) setupPermissions(home string) {
	path := filepath.Join(home, ".celeste", "permissions.json")
	pc, err := permissions.LoadConfig(path)
	if err != nil {
		// LoadConfig already treats a missing file as defaults with a nil
		// error, so reaching here means the file exists but is unreadable
		// or malformed: warn, since MCP-chat Trust mode would otherwise
		// silently drop the user's deny rules (#187).
		e.warn("permissions config %s is invalid, using defaults: %v", path, err)
		d := permissions.DefaultConfig()
		pc = &d
	}
	if e.Mode == ModeMCPChat {
		// Headless: calling the tool is the approval, but the user's deny
		// rules still apply (#187).
		pc.Mode = permissions.ModeTrust
	}
	e.permConfig = *pc
	e.Checker = permissions.NewChecker(*pc)
	if e.Mode == ModeChat {
		e.Checker.SetConfigPath(path) // "always allow" from the modal persists
	}
	e.Registry.SetPermissionChecker(e.Checker)
}

// setupHooks loads the session's hooks (F0). Only an interactive TUI whose
// stdin and stderr are terminals can approve repo hooks; every other mode
// passes a nil Approve, so untrusted hooks are skipped with a warning and
// never auto-approved.
func (e *Env) setupHooks(home string) {
	var approve hooks.ApproveFunc
	if e.Mode == ModeChat && hooks.IsTerminal(os.Stdin) && hooks.IsTerminal(os.Stderr) {
		approve = hooks.PromptApprover(os.Stdin, os.Stderr)
	}
	runner, err := hooks.Load(hooks.Options{
		Workspace: e.Workspace,
		Home:      home,
		SessionID: e.opts.SessionID,
		Approve:   approve,
		Warn:      e.opts.Warn,
	})
	if err != nil {
		e.opts.Warn(hooks.DisabledWarning(err))
		return
	}
	// Tool hooks run in the registry, once, for every call in every mode.
	if th := runner.ToolHooks(); th != nil {
		e.Registry.SetHookRunner(th)
	}
	e.Hooks = runner
}

// StartSession fires SessionStart for a top-level run and adds its context
// to the project context, under the TUI's heading. Call it before
// SystemPrompt; nested runs (subagents, orchestrator lanes) don't call it.
func (e *Env) StartSession(ctx context.Context, source string) {
	if e.Hooks == nil {
		return
	}
	start := e.Hooks.SessionStart(ctx, source)
	if start.AdditionalContext == "" {
		return
	}
	e.ProjectContext = strings.TrimSpace(e.ProjectContext + "\n\n# Session Start Hook Context\n\n" + start.AdditionalContext)
}

// Trust switches the checker to trust mode, keeping the deny rules. An
// adopter calls it when invoking the run is the approval (agent -auto-approve,
// subagents).
func (e *Env) Trust() {
	e.permConfig.Mode = permissions.ModeTrust
	e.Checker = permissions.NewChecker(e.permConfig)
	e.Registry.SetPermissionChecker(e.Checker)
}

func (e *Env) setupMCP(ws, home string) {
	e.MCP = mcp.NewManagerMulti(mcp.DiscoverConfigPaths(ws, home), e.Registry)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.MCP.Start(ctx); err != nil {
		e.warn("MCP initialization failed: %v", err)
	}
}

// setupContext builds ProjectContext exactly as the TUI does: grimoire, then
// "# Project Memories", then "# Code Graph".
func (e *Env) setupContext(ws string) {
	var text string
	if g, err := grimoire.LoadAll(ws); err != nil {
		e.warn("failed to load .grimoire: %v", err)
	} else if g != nil && !g.IsEmpty() {
		text = g.Render()
	}
	store := memories.NewStore(ws)
	if idx, err := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md")); err == nil && len(idx.Entries()) > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += "# Project Memories\n\n" + idx.Render()
	}
	e.GitSnapshot = e.captureGit(ws)
	if summary := e.setupCodeGraph(ws); summary != "" {
		if text != "" {
			text += "\n\n"
		}
		text += "# Code Graph\n\n" + summary
	}
	e.ProjectContext = text
}

func (e *Env) captureGit(ws string) string {
	done := make(chan *grimoire.GitSnapshot, 1)
	go func() { done <- grimoire.CaptureGitSnapshot(ws) }()
	select {
	case snap := <-done:
		if snap != nil {
			return snap.FormatForPrompt()
		}
	case <-time.After(5 * time.Second):
		e.warn("git snapshot timed out, skipping")
	}
	return ""
}

func (e *Env) setupCodeGraph(ws string) string {
	idx, err := codegraph.NewIndexer(ws, codegraph.DefaultIndexPath(ws))
	if err != nil {
		e.warn("code graph init failed: %v", err)
		return ""
	}
	// Cancellable so a Close that arrives after this update outlives its 10s
	// wait can stop it instead of blocking indexing.Wait() unbounded.
	ctx, cancel := context.WithCancel(context.Background())
	e.indexCancel = cancel
	done := make(chan error, 1)
	e.indexing.Add(1)
	go func() {
		defer e.indexing.Done()
		done <- idx.UpdateWithContext(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			e.warn("code graph update failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		e.warn("code graph update timed out (10s), skipping")
	}
	builtin.RegisterCodeGraphTools(e.Registry, idx)
	e.Indexer = idx
	return idx.ProjectSummary()
}

// SystemPrompt composes the mode's system prompt: persona (unless skipped),
// contract (agent mode), sliders, project context and git state.
func (e *Env) SystemPrompt(contract string, sliders *config.SliderConfig) string {
	pm := prompts.ModeChat
	if e.Mode == ModeAgent {
		pm = prompts.ModeAgent
	}
	return prompts.Compose(prompts.ComposeOptions{
		Mode:           pm,
		SkipPersona:    e.skipPersona,
		Contract:       contract,
		Sliders:        sliders,
		ProjectContext: e.ProjectContext,
		GitSnapshot:    e.GitSnapshot,
	})
}

// Close stops MCP clients and closes the code graph, cancelling a code-graph
// update still running past its 10s Setup wait so this returns promptly
// instead of blocking on it. Safe to call more than once (and concurrently);
// only the first call runs.
func (e *Env) Close() {
	e.closeOnce.Do(func() {
		if e.indexCancel != nil {
			e.indexCancel()
		}
		if e.MCP != nil {
			_ = e.MCP.Stop()
		}
		e.indexing.Wait()
		if e.Indexer != nil {
			_ = e.Indexer.Close()
		}
	})
}
