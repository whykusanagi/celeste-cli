package loop

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
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

// ToolDiscoveryThreshold turns on discovery mode (MCP tools hidden until
// find_tools activates them) once the registry is this big.
const ToolDiscoveryThreshold = 40

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
	ProjectContext string // grimoire, context files, code-graph summary
	GitSnapshot    string
	// Memories is the "# Project Memories" block. It follows git in the
	// prompt (W5 ruling 8); GrimoireContext still shows it.
	Memories         string
	GrimoireContext  string // grimoire and "# Project Memories" (the /grimoire view)
	CodeGraphSummary string // the code graph's project summary (the /index view)
	// Rules are the stream rules for this workspace (2.0 W3): built-ins,
	// ~/.celeste/rules/*.md, then the grimoire's "## Stream Rules" (a repo
	// grimoire's only once trusted, like a repo hook).
	Rules *rules.Set
	// SandboxPolicy is bash's OS sandbox (2.0 W4): defaults, the user's
	// "sandbox" settings, then the workspace's (loosening only once
	// trusted). A nested Env in the same workspace inherits it; one in
	// another workspace resolves its own.
	SandboxPolicy sandbox.Policy

	opts        SetupOptions
	userSandbox *config.Sandbox // the user's "sandbox" settings, for nested Envs
	// sandboxTrust is the workspace "sandbox" object whose loosening this
	// Env trusted (its own approval, or its parent's for a lane under the
	// parent's workspace); a nested Env under this workspace with the same
	// object reuses it. "" when none.
	sandboxTrust string
	window       int               // cfg's model's resolved context window (W5 guard); 0 = unknown
	approve      hooks.ApproveFunc // resolved once by approver
	approveSet   bool
	permConfig   permissions.PermissionConfig
	indexing     sync.WaitGroup     // a code-graph update that outlived its timeout
	indexCancel  context.CancelFunc // stops that update; nil until setupCodeGraph runs
	closeOnce    sync.Once

	home string // the user's home: children load skills, permissions and hooks from it
	// shared counts the Envs (a Setup Env and its Nested children) using one
	// set of MCP clients and one code graph; the last Close frees them.
	shared    *sharedRes
	nested    bool // built by Nested; a child never nests further
	ownsIndex bool // a child in another workspace closes its own code graph
	// indexOwner is the Env whose code graph this one uses (itself when it
	// built one); indexCtx is that update context, cancelled by Close.
	// indexSem (capacity 1) is held while an update of this Env's code
	// graph runs, so updates never overlap (the indexer's parsers are not
	// concurrency-safe); a channel, so a child can wait for it with a
	// deadline.
	indexOwner *Env
	indexCtx   context.Context
	indexSem   chan struct{}
	lifeMu     sync.Mutex // guards closed against a concurrent Nested
	closed     bool
}

// SetupOptions carries what only the adopter knows.
type SetupOptions struct {
	SessionID string       // hooks' session_id and checkpoint session; "" = "<mode>-<pid>-<start nanos>"
	Warn      func(string) // setup and hook warnings; nil = stderr
	// Notice gets timing notices (a git snapshot or code-graph update that
	// ran out of time). They depend on the machine, not the configuration.
	// nil = Warn.
	Notice func(string)
	// Approve asks the person to trust a repo hook. Only ModeChat uses it
	// (non-interactive modes never approve, F0); nil there means a terminal
	// prompt when stdin and stderr are terminals.
	Approve hooks.ApproveFunc
	// GlobalMCPOnly skips the workspace's MCP configs in ModeChat too, as
	// every other mode does: a chat whose user cannot be asked before a
	// repo's server starts (ACP, where the editor passes its own servers).
	GlobalMCPOnly bool
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

	if opts.Warn == nil {
		opts.Warn = func(s string) { fmt.Fprintln(os.Stderr, "Warning: "+s) }
	}
	home := userHome()
	if home == "" {
		opts.Warn("no home directory: using the default permissions (nothing saved) with no custom skills or home-level hooks, MCP servers or stream rules")
	}
	if opts.Notice == nil {
		opts.Notice = opts.Warn
	}
	if opts.SessionID == "" {
		// The start time too: a later process can get the same pid, and
		// would then share this run's checkpoints (and /undo them).
		opts.SessionID = fmt.Sprintf("%s-%d-%s", mode, os.Getpid(), config.UniqueNanoID())
	}
	env := &Env{Mode: mode, Workspace: ws, ToolMode: tools.ModeChat, opts: opts, home: home}
	env.window, _ = config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit, cfg.APIKey)
	if mode == ModeAgent {
		env.ToolMode = tools.ModeAgent
	}
	env.Files = checkpoints.NewFileTracker()
	// Checkpoints are the run's session's (2.0 F4): the chat session, the
	// agent run, the MCP chat Env; <mode>-<pid>-<nanos> when none was given.
	env.Snapshots = checkpoints.NewSnapshotManager(opts.SessionID)
	env.Registry = tools.NewRegistry()
	env.userSandbox = cfg.Sandbox
	env.SandboxPolicy = env.resolveSandbox(env.userSandbox)
	policy := env.SandboxPolicy
	builtin.RegisterAll(env.Registry, ws, nil, env.Files, env.Snapshots, &policy)
	env.loadCustomTools()
	env.setupPermissions(home)
	env.setupHooks(home)
	env.setupMCP(ws, home)
	if env.Registry.Count() > ToolDiscoveryThreshold {
		env.Registry.SetDiscoveryMode(true)
	}
	env.setupContext(ws)
	env.shared = newShared(env.closeAll)
	return env, nil
}

func (e *Env) warn(format string, args ...any) {
	e.opts.Warn(fmt.Sprintf(format, args...))
}

// userHome is the user's home directory, or "" when it cannot be
// resolved to an absolute path (HOME unset in some CI and service
// environments). Every home-level file is skipped then: joining "" with
// .celeste would read the current directory, normally the repository.
func userHome() string {
	h, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(h) {
		return ""
	}
	return h
}

// loadCustomTools loads the user's custom skills from the home directory,
// none without one.
func (e *Env) loadCustomTools() {
	if e.home == "" {
		return
	}
	if err := e.Registry.LoadCustomTools(filepath.Join(e.home, ".celeste", "skills")); err != nil {
		e.warn("custom skills: %v", err)
	}
}

func (e *Env) setupPermissions(home string) {
	if home == "" {
		// No home: the defaults, never a workspace-relative file.
		d := permissions.DefaultConfig()
		e.applyPermissions(&d)
		return
	}
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
	e.applyPermissions(pc)
}

// applyPermissions installs pc as the run's permission checker.
func (e *Env) applyPermissions(pc *permissions.PermissionConfig) {
	if e.Mode == ModeMCPChat {
		// Headless: calling the tool is the approval, but the user's deny
		// rules still apply (#187).
		pc.Mode = permissions.ModeTrust
	}
	e.permConfig = *pc
	e.Checker = permissions.NewChecker(*pc)
	if e.Mode == ModeChat {
		e.PersistRules() // "always allow" from the modal persists
	}
	e.Registry.SetPermissionChecker(e.Checker)
}

// setupHooks loads the session's hooks (F0). Only an interactive TUI whose
// stdin and stderr are terminals can approve repo hooks; every other mode
// passes a nil Approve, so untrusted hooks are skipped with a warning and
// never auto-approved.
func (e *Env) setupHooks(home string) {
	if home == "" {
		// hooks.Load would resolve the home itself, and could land on a
		// relative one: no hooks without a home (Setup warned).
		return
	}
	runner, err := hooks.Load(hooks.Options{
		Workspace: e.Workspace,
		Home:      home,
		SessionID: e.opts.SessionID,
		Approve:   e.approver(),
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

// approver is who may trust repo hooks and repo stream rules: the
// interactive TUI's approver (or a terminal prompt when stdin and stderr are
// terminals), nil in every other mode. Resolved once, so one terminal
// prompt reads stdin for both.
func (e *Env) approver() hooks.ApproveFunc {
	if e.approveSet {
		return e.approve
	}
	e.approveSet = true
	if e.Mode == ModeChat {
		e.approve = e.opts.Approve
		if e.approve == nil && hooks.IsTerminal(os.Stdin) && hooks.IsTerminal(os.Stderr) {
			e.approve = hooks.PromptApprover(os.Stdin, os.Stderr)
		}
	}
	return e.approve
}

// StartSession fires SessionStart for a top-level run and adds its context
// to the project context, under the TUI's heading. Call it before
// SystemPrompt; nested runs (subagents, orchestrator lanes) don't call it.
// A shared Env (MCP chat) uses SessionStartContext instead.
func (e *Env) StartSession(ctx context.Context, source string) {
	e.ProjectContext = withSessionContext(e.ProjectContext, e.SessionStartContext(ctx, source))
}

// SessionStartContext fires SessionStart and returns its additional
// context, leaving the Env unchanged, so several runs sharing one Env each
// get their own session. Pass the result as PromptOptions.Session.
func (e *Env) SessionStartContext(ctx context.Context, source string) string {
	if e.Hooks == nil {
		return ""
	}
	return e.Hooks.SessionStart(ctx, source).AdditionalContext
}

func withSessionContext(project, session string) string {
	if session == "" {
		return project
	}
	return strings.TrimSpace(project + "\n\n# Session Start Hook Context\n\n" + session)
}

// PersistRules makes the checker save "always allow" and "always deny"
// answers to permissions.json, as the chat's does. An adopter calls it only
// when the answers come from the user's interactive prompt (the TUI modal
// behind /agent and /orchestrate lanes); headless runs never write.
// A rule that can't be saved (a damaged permissions.json is never
// overwritten) still applies for this run, and the failure is warned.
func (e *Env) PersistRules() {
	if e.home == "" {
		return // nowhere to save: never a workspace-relative file
	}
	e.Checker.SetConfigPath(filepath.Join(e.home, ".celeste", "permissions.json"))
	e.Checker.SetPersistWarn(func(err error) { e.warn("%v", err) })
}

// Trust switches the checker to trust mode, keeping the deny rules. An
// adopter calls it when invoking the run is the approval (agent -auto-approve,
// subagents).
func (e *Env) Trust() {
	e.permConfig.Mode = permissions.ModeTrust
	e.Checker = permissions.NewChecker(e.permConfig)
	e.Registry.SetPermissionChecker(e.Checker)
}

// RefreshDiscovery turns tool discovery on when tools added after Setup
// (the chat's config-backed skills, collections, spawn_agent, post_message)
// take the registry past ToolDiscoveryThreshold. It never turns it off.
func (e *Env) RefreshDiscovery() {
	if e.Registry.Count() > ToolDiscoveryThreshold {
		e.Registry.SetDiscoveryMode(true)
	}
}

// setupMCP starts the configured MCP servers. Only the TUI loads workspace
// configs (<ws>/.mcp.json, <ws>/.celeste/mcp.json), and not with
// GlobalMCPOnly: every other mode runs without an interactive user, and a
// repo's config would otherwise run an arbitrary command unasked. Even in
// the chat, an enabled workspace server starts only once approved
// (admitMCP): the repo sets "enabled" itself.
func (e *Env) setupMCP(ws, home string) {
	var paths []string
	for _, p := range mcp.DiscoverConfigPaths(ws, home) {
		// Without a home the home-level candidates are relative: skip
		// them rather than read the current directory.
		if filepath.IsAbs(p) {
			paths = append(paths, p)
		}
	}
	if e.Mode != ModeChat || e.opts.GlobalMCPOnly {
		paths = e.globalMCPConfigs(paths, home)
	}
	e.MCP = mcp.NewManagerMulti(paths, e.Registry)
	e.MCP.SetAdmit(e.admitMCP(paths, home))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.MCP.Start(ctx); err != nil {
		e.warn("MCP initialization failed: %v", err)
	}
}

// admitMCP decides, before any server starts (and before Start's
// connection timeout runs), which enabled workspace servers may start: one
// approved with its current command, args, env and url, or approved now by
// the person. Home-level servers are the user's own and always start. The
// returned func admits a workspace server only if its definition still
// hashes to the one approved.
func (e *Env) admitMCP(paths []string, home string) func(string, mcp.ServerConfig) bool {
	approved := map[string]string{} // server name -> approved TrustHash
	admit := func(name string, sc mcp.ServerConfig) bool {
		if mcp.IsGlobalConfig(home, sc.Origin) {
			return true
		}
		h, ok := approved[name]
		return ok && h == sc.TrustHash()
	}
	cfg, err := mcp.LoadMerged(paths)
	if err != nil {
		return admit // Start reports the error and starts nothing
	}
	var store *hooks.TrustStore
	for _, name := range slices.Sorted(maps.Keys(cfg.Servers)) {
		sc := cfg.Servers[name]
		if !sc.Enabled || mcp.IsGlobalConfig(home, sc.Origin) {
			continue
		}
		src := hooks.MCPSource(sc.Origin, name, sc.TrustSummary(), sc.TrustHash())
		if home == "" {
			e.warnMCPSkipped(src, "no home directory for the trust store")
			continue
		}
		if store == nil {
			store = hooks.LoadTrust(home)
			if err := store.Err(); err != nil {
				e.warn("MCP: %v; repository MCP servers stay unapproved until it is fixed or removed", err)
			}
		}
		if e.trustMCP(store, src) {
			approved[name] = src.Hash
		}
	}
	return admit
}

// trustMCP reports whether a workspace MCP server may start: approved in
// the store with this definition, or approved now through the chat's
// approver (and stored). A declined one is skipped without asking until
// its definition changes (#411).
func (e *Env) trustMCP(store *hooks.TrustStore, src hooks.Source) bool {
	var approve hooks.ApproveFunc
	if store.Err() == nil {
		approve = e.approver()
	}
	run, why, err := hooks.Decide(store, src, approve)
	if err != nil {
		e.warn("MCP: server %s: %v", strconv.Quote(hooks.MCPServerName(src)), err)
	}
	if !run {
		e.warnMCPSkipped(src, why)
	}
	return run
}

func (e *Env) warnMCPSkipped(src hooks.Source, why string) {
	name := hooks.MCPServerName(src)
	if why == "declined" {
		e.warn("MCP: not starting server %s from %s (declined): run `celeste mcp trust %s` to approve it", strconv.Quote(name), strconv.Quote(hooks.SourceFile(src)), hooks.SafeText(name))
		return
	}
	e.warn("MCP: not starting server %s from %s (%s): a repository's MCP servers start only once approved; run `celeste mcp trust %s` to approve it, or connect it from /mcp", strconv.Quote(name), strconv.Quote(hooks.SourceFile(src)), why, hooks.SafeText(name))
}

// globalMCPConfigs keeps the home-level configs in paths and warns once about
// the workspace ones it drops.
func (e *Env) globalMCPConfigs(paths []string, home string) []string {
	kept, skipped := mcp.SplitGlobal(paths, home)
	if len(skipped) > 0 {
		quoted := make([]string, len(skipped))
		for i, p := range skipped {
			quoted[i] = strconv.Quote(p)
		}
		e.warn("skipping repo MCP config %s: repo MCP servers don't start in non-interactive runs (move the server to a global config such as ~/.celeste/mcp.json to use it)", strings.Join(quoted, ", "))
	}
	return kept
}

// setupContext builds ProjectContext exactly as the TUI does: grimoire, then
// "# Project instructions" (AGENTS.md / CLAUDE.md from the git root down to
// the workspace, 2.0 W4), then "# Code Graph". "# Project Memories" goes to
// Memories, which the prompt puts after git (W5 ruling 8); GrimoireContext,
// the /grimoire view, keeps it after the instructions, as before.
func (e *Env) setupContext(ws string) {
	var text string
	var ruleSections []rules.Section
	if g, err := grimoire.LoadAll(ws); err != nil {
		e.warn("failed to load .grimoire: %v", err)
	} else if g != nil {
		if !g.IsEmpty() {
			text = g.Render()
		}
		for _, sec := range g.StreamRules {
			ruleSections = append(ruleSections, rules.Section{Source: sec.Source, Body: sec.Body})
		}
	}
	files, fileWarns := grimoire.ContextFiles(ws)
	for _, w := range fileWarns {
		e.warn("%s", w)
	}
	if section := grimoire.RenderContextFiles(files); section != "" {
		if text != "" {
			text += "\n\n"
		}
		text += section
	}
	warn := func(s string) { e.warn("%s", s) }
	e.Rules = rules.Load(e.home, rules.Trusted(e.home, ruleSections, e.approver(), warn), warn)
	store := memories.NewStore(ws)
	if idx, err := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md")); err == nil && len(idx.Entries()) > 0 {
		e.Memories = "# Project Memories\n\n" + idx.Render()
	}
	e.GrimoireContext = text
	if e.Memories != "" {
		if e.GrimoireContext != "" {
			e.GrimoireContext += "\n\n"
		}
		e.GrimoireContext += e.Memories
	}
	e.GitSnapshot = e.captureGit(ws)
	if summary := e.setupCodeGraph(ws); summary != "" {
		e.CodeGraphSummary = summary
		if text != "" {
			text += "\n\n"
		}
		text += "# Code Graph\n\n" + summary
	}
	e.ProjectContext = text
}

// Setup's waits and its code-graph update, as vars so tests can shorten the
// waits and block the update.
var (
	gitSnapshotTimeout = 5 * time.Second
	codeGraphTimeout   = 10 * time.Second
	updateCodeGraph    = func(ctx context.Context, idx *codegraph.Indexer) error { return idx.UpdateWithContext(ctx) }
)

func (e *Env) captureGit(ws string) string {
	done := make(chan *grimoire.GitSnapshot, 1)
	go func() { done <- grimoire.CaptureGitSnapshot(ws) }()
	select {
	case snap := <-done:
		if snap != nil {
			return snap.FormatForPrompt()
		}
	case <-time.After(gitSnapshotTimeout):
		e.opts.Notice("git snapshot timed out, skipping")
	}
	return ""
}

// reportIndexUpdate reports a failed code-graph update. An index that
// another celeste process (or the MCP server) is writing is not a failure:
// the update skipped, and that run brings the index up to date (#392).
func (e *Env) reportIndexUpdate(err error) {
	switch {
	case errors.Is(err, codegraph.ErrIndexBusy):
		e.opts.Notice("code graph is being indexed by another celeste process, continuing with the index as it is")
	case err != nil:
		e.warn("code graph update failed: %v", err)
	}
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
	e.indexCtx, e.indexCancel, e.indexOwner = ctx, cancel, e
	done := make(chan error, 1)
	e.indexing.Add(1)
	e.indexSem = make(chan struct{}, 1)
	e.indexSem <- struct{}{} // the update releases it: a child's refresh never overlaps it
	go func() {
		defer e.indexing.Done()
		defer func() { <-e.indexSem }()
		done <- updateCodeGraph(ctx, idx)
	}()
	select {
	case err := <-done:
		e.reportIndexUpdate(err)
	case <-time.After(codeGraphTimeout):
		e.opts.Notice(fmt.Sprintf("code graph update timed out (%s), skipping", codeGraphTimeout))
	}
	builtin.RegisterCodeGraphTools(e.Registry, idx)
	e.Indexer = idx
	return idx.ProjectSummary()
}

// PromptOptions is what one system prompt adds to the Env's own inputs.
type PromptOptions struct {
	// Contract is the agent operating contract (agent mode only).
	Contract string
	// Sliders overrides slider.json (a subagent's persona override).
	Sliders *config.SliderConfig
	// Session is one run's SessionStart context; it never enters the Env.
	Session string
	// Level is the persona level: "" is the mode's profile (chat full,
	// agent spine); prompts.PersonaOff for typed explore and review
	// subagents (2.0 W4e) and orchestrator lanes.
	Level prompts.PersonaLevel
	// Window is the run's model's context window; 0 uses the Env's.
	Window int
}

// SystemPrompt composes the mode's system prompt (W5): the persona profile
// as the byte-stable Static part, stepped down for the window, then
// sliders, identity, mode rules, project context, git, memories and the
// date. A Notice in the result is the small-window guard's one-time
// message; the caller decides where it shows (W5 ruling 7).
func (e *Env) SystemPrompt(o PromptOptions) prompts.Prompt {
	pm := prompts.ModeChat
	if e.Mode == ModeAgent {
		pm = prompts.ModeAgent
	}
	window := o.Window
	if window <= 0 {
		window = e.window
	}
	return prompts.Compose(prompts.ComposeOptions{
		Mode:           pm,
		PersonaLevel:   o.Level,
		Window:         window,
		Contract:       o.Contract,
		Sliders:        o.Sliders,
		ProjectContext: withSessionContext(e.ProjectContext, o.Session),
		GitSnapshot:    e.GitSnapshot,
		Memories:       e.Memories,
	})
}

// Close releases this Env. A Setup Env and its Nested children share MCP
// clients and a code graph, which stop when the last of them closes, so a
// child still running keeps them open after its parent closes. A child in
// another workspace also closes its own code graph. A code-graph update
// still running is cancelled before it is waited on, so this returns
// promptly. Safe to call more than once (and concurrently).
func (e *Env) Close() {
	e.closeOnce.Do(func() {
		e.lifeMu.Lock()
		e.closed = true
		e.lifeMu.Unlock()
		if e.shared == nil { // built by hand in tests
			e.closeAll()
			return
		}
		if e.ownsIndex {
			e.closeIndex()
		}
		e.shared.release()
	})
}

// closeAll stops the MCP clients and closes the code graph, cancelling a
// running update first (F2a Task 7 fix).
func (e *Env) closeAll() {
	if e.indexCancel != nil {
		e.indexCancel()
	}
	if e.MCP != nil {
		_ = e.MCP.Stop()
	}
	e.closeIndex()
}

// closeIndex cancels and waits for this Env's code-graph updates (its own
// and its children's refreshes) and closes the code graph it built.
func (e *Env) closeIndex() {
	if e.indexCancel != nil {
		e.indexCancel()
	}
	e.indexing.Wait()
	if e.Indexer != nil {
		_ = e.Indexer.Close()
	}
}
