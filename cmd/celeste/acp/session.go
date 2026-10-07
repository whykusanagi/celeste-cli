package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// mcpConnectTimeout bounds connecting one client MCP server at session/new.
const mcpConnectTimeout = 10 * time.Second

// session is one ACP session: a celeste session (its store record and
// history) with the chat's Env for the editor's folder.
type session struct {
	id, cwd      string
	cfg          *config.Config
	env          *loop.Env
	client       *llm.Client
	systemPrompt string
	store        *config.Session
	// compactor keeps the history inside the window (ruling 12); its
	// budget spans the session's prompts.
	compactor *compactor

	// running is set while a prompt runs (ruling 2: one at a time).
	running atomic.Bool
	// spillSeq numbers spilled tool results across the session's prompts
	// (one Loop each), so a repeated call ID never overwrites a spill.
	spillSeq atomic.Int64
	// permSeq numbers permission asks that came without a call ID.
	permSeq atomic.Int64

	mu sync.Mutex
	// history is the conversation; only the running prompt replaces it.
	history []tui.ChatMessage
	// cancel stops the running prompt (session/cancel); nil when idle.
	cancel context.CancelFunc
	// arrivedN counts prompts received but not started; cancelN is how
	// many of them a session/cancel cancelled before they started.
	arrivedN, cancelN int
	// allow holds the tools the editor's user allowed always (ruling 8).
	allow map[string]bool
	// pendingHooks are the untrusted repo hook sources Setup skipped
	// (ruling 9); the session's first prompt asks the editor's user about
	// them, and askedHooks is set once every one was answered.
	pendingHooks []hooks.Source
	askedHooks   bool
	// trusted are the sources the editor's user trusted in this session:
	// Setup runs them even if storing the trust failed. skipped are the
	// ones the user skipped: never asked about again in this session.
	trusted, skipped []hooks.Source
	// mcpServers are the editor's MCP servers, connected again when the
	// Env is rebuilt.
	mcpServers []McpServer
	// notice is the small-window guard's message (W5 ruling 7), shown
	// once at the start of the session's first prompt; "" once shown.
	notice string
}

// newSession answers session/new (rulings 3-4): a celeste session whose ID
// is the ACP sessionId, the chat's Env for cwd, the client's MCP servers
// on it, and the chat's system prompt.
func (a *Agent) newSession(ctx context.Context, p NewSessionParams) (*session, *RPCError) {
	cwd, rerr := checkCwd(p.Cwd)
	if rerr != nil {
		return nil, rerr
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, &RPCError{Code: CodeInternal, Message: "loading celeste's config: " + err.Error()}
	}
	store := a.newStore()
	s := &session{id: store.ID, cwd: cwd, cfg: cfg, store: store, allow: map[string]bool{}, mcpServers: p.McpServers}
	if rerr := a.setupEnv(ctx, s); rerr != nil {
		return nil, rerr
	}
	store.SetWorkspace(cwd)
	store.Metadata["source"] = "acp"
	if err := a.saveStore(store); err != nil {
		s.env.Close()
		return nil, &RPCError{Code: CodeInternal, Message: "saving the session: " + err.Error()}
	}
	if !a.addSession(s) {
		s.env.Close()
		return nil, &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	return s, nil
}

// checkCwd requires an absolute path to an existing directory.
func checkCwd(cwd string) (string, *RPCError) {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", &RPCError{Code: CodeInvalidParams, Message: fmt.Sprintf("cwd must be an absolute path, got %q", cwd)}
	}
	fi, err := os.Stat(cwd)
	if err != nil || !fi.IsDir() {
		return "", &RPCError{Code: CodeInvalidParams, Message: fmt.Sprintf("cwd %q is not a directory", cwd)}
	}
	return filepath.Clean(cwd), nil
}

// setupEnv builds the session's Env, client and system prompt (ruling 4).
// Warnings go to the log, never to stdout. Untrusted repo hooks are
// recorded and skipped: Approve never blocks (ruling 9). Repo MCP configs
// are skipped: only the user's global ones and the editor's start.
func (a *Agent) setupEnv(ctx context.Context, s *session) *RPCError {
	warn := func(msg string) { a.logf("acp: session %s: %s", s.id, msg) }
	env, err := loop.Setup(loop.ModeChat, s.cfg, s.cwd, loop.SetupOptions{
		SessionID: s.id,
		Warn:      warn,
		Notice:    warn,
		Approve:   s.recordHook,
		// A repo's MCP servers would start before the editor's user could
		// be asked; the editor passes the servers it wants instead.
		GlobalMCPOnly: true,
	})
	if err != nil {
		return &RPCError{Code: CodeInternal, Message: "setting up the session: " + err.Error()}
	}
	a.connectMCP(ctx, s.id, s.cwd, env, s.mcpServers)
	env.RefreshDiscovery()

	client := llm.NewClient(llm.ConfigFrom(s.cfg), env.Registry)
	client.SetToolMode(tools.ModeChat)
	env.StartSession(ctx, "acp")
	// The persona steps down for a small window (W5 guard); its notice
	// goes to the editor with the first prompt, never to stderr.
	window, _ := config.ResolveContextLimit(s.cfg.BaseURL, s.cfg.Model, s.cfg.ContextLimit, s.cfg.APIKey)
	sp := env.SystemPrompt(loop.PromptOptions{Window: window})
	prompt := sp.String()
	client.SetSystemPromptParts(sp.Static, sp.Dynamic)

	pruned, err := compact.DefaultStore()
	if err != nil {
		warn("pruned tool results cannot be kept (" + err.Error() + "); pruning is off")
		pruned = nil
	}
	summarize := agent.SmallModelSummarizer(llm.ConfigFrom(s.cfg), s.cfg.ResolveSmallModel())

	comp := newCompactor(s.cfg, prompt, pruned, summarize, env.Hooks, a.logf)
	// The tool schemas count as fixed overhead too; on a small window they
	// are a fitted core set, and the editor is told once (#310).
	comp.budget.ToolDefinitionTokens = compact.DefinitionTokens(client.GetSkills())
	notice := sp.Notice
	if tn := client.TakeToolNotice(); tn != "" {
		if notice != "" {
			notice += "\n" + prompts.NoticePrefix
		}
		notice += tn
	}
	s.mu.Lock()
	s.env, s.client, s.systemPrompt, s.notice, s.compactor = env, client, prompt, notice, comp
	s.mu.Unlock()
	return nil
}

// sameSource reports a and b are the same hook file with the same content.
func sameSource(a, b hooks.Source) bool {
	return a.Path == b.Path && a.Kind == b.Kind && a.Hash == b.Hash
}

func hasSource(list []hooks.Source, src hooks.Source) bool {
	for _, s := range list {
		if sameSource(s, src) {
			return true
		}
	}
	return false
}

// recordHook is the Env's hook approver: it never blocks. A source the
// editor's user trusted in this session runs; any other is recorded for
// the first prompt to ask about and answered no, so Setup skips it (F0,
// ruling 9).
func (s *session) recordHook(src hooks.Source, _ hooks.TrustStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasSource(s.trusted, src) {
		return true
	}
	if !hasSource(s.skipped, src) && !hasSource(s.pendingHooks, src) {
		s.pendingHooks = append(s.pendingHooks, src)
	}
	return false
}

// connectMCP connects the client's stdio MCP servers (ruling 4), started
// in the session's cwd. Failures are logged and the session goes on; http
// and sse servers (not advertised) are ignored, and so is one named like a
// global server already connected (logged).
func (a *Agent) connectMCP(ctx context.Context, sid, cwd string, env *loop.Env, servers []McpServer) {
	for _, srv := range servers {
		if srv.Type != "" && srv.Type != "stdio" {
			a.logf("acp: session %s: ignoring %s MCP server %q (only stdio is supported)", sid, srv.Type, srv.Name)
			continue
		}
		if srv.Name == "" || srv.Command == "" {
			a.logf("acp: session %s: ignoring an MCP server without a name or command", sid)
			continue
		}
		if env.MCP.IsConnected(srv.Name) {
			a.logf("acp: session %s: MCP server %q is already configured globally; the editor's entry is ignored", sid, srv.Name)
			continue
		}
		envVars := map[string]string{}
		for _, v := range srv.Env {
			envVars[v.Name] = v.Value
		}
		cctx, cancel := context.WithTimeout(ctx, mcpConnectTimeout)
		err := env.MCP.Connect(cctx, srv.Name, mcp.ServerConfig{
			Enabled: true, Transport: "stdio", Command: srv.Command, Args: srv.Args, Env: envVars, Dir: cwd,
		})
		cancel()
		if err != nil {
			a.logf("acp: session %s: MCP server %q: %v", sid, srv.Name, err)
		}
	}
}

// close releases the session's Env.
func (s *session) close() {
	s.mu.Lock()
	env := s.env
	s.mu.Unlock()
	if env != nil {
		env.Close()
	}
}

// cancelPrompt cancels the running prompt, if any, and the prompts that
// arrived before the cancel but have not started yet. The cancel func is
// called under s.mu, so a prompt's end either sees its context cancelled
// or has already uninstalled the func (the turn ended before the cancel).
func (s *session) cancelPrompt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelN = s.arrivedN
	if s.cancel != nil {
		s.cancel()
	}
}

// arrived counts a session/prompt read off the connection.
func (s *session) arrived() {
	s.mu.Lock()
	s.arrivedN++
	s.mu.Unlock()
}

// dequeue uncounts an arrived prompt that will not run; it reports
// whether a session/cancel had cancelled it.
func (s *session) dequeue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dequeueLocked()
}

func (s *session) dequeueLocked() bool {
	if s.arrivedN > 0 {
		s.arrivedN--
	}
	if s.cancelN > 0 {
		s.cancelN--
		return true
	}
	return false
}

// begin starts a prompt: it uncounts it and installs its cancel func in
// one step, so a session/cancel either finds the func or marks the prompt
// cancelled. False when a cancel came first.
func (s *session) begin(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dequeueLocked() {
		return false
	}
	s.cancel = cancel
	return true
}

// end uninstalls a prompt's cancel func; it reports whether a
// session/cancel reached the prompt before that. A cancel read after end
// finds no prompt: that turn had already ended.
func (s *session) end(pctx context.Context) (cancelled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = nil
	return pctx.Err() != nil
}

// maxToolContent caps the tool result text a tool_call_update carries
// (ruling 6); the model gets the whole result.
const maxToolContent = 8 << 10

// prompt runs one session/prompt on the loop (rulings 2, 6, 10): one at a
// time per session, the loop's events streamed as session/update, and the
// stop reason answered when the turn ends.
func (s *session) prompt(ctx context.Context, a *Agent, text string) (*PromptResult, *RPCError) {
	if !s.running.CompareAndSwap(false, true) {
		s.dequeue()
		return nil, &RPCError{Code: CodeBusy, Message: "a prompt is already running in this session"}
	}
	defer s.running.Store(false)
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !s.begin(cancel) {
		// session/cancel came after this prompt but before it started.
		return &PromptResult{StopReason: StopCancelled}, nil
	}
	s.askHooks(pctx, a)
	s.mu.Lock()
	history := append(append([]tui.ChatMessage(nil), s.history...),
		tui.ChatMessage{Role: "user", Content: text, Timestamp: time.Now()})
	notice := s.notice
	s.notice = ""
	s.mu.Unlock()
	if notice != "" {
		// Shown to the editor's user only; the model never sees it.
		s.update(a, AgentMessageChunk(prompts.NoticePrefix+notice+"\n\n"))
	}
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()

	st := newPromptState()
	l := s.newLoop(a, st)
	events := l.Events()
	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		for ev := range events {
			s.onEvent(a, st, ev)
			if ev.Kind == loop.EventDone {
				return
			}
		}
	}()
	msgs, res, err := l.Run(pctx, history)
	<-pumped

	s.mu.Lock()
	s.history = msgs
	s.mu.Unlock()
	s.save(a, msgs)
	if s.end(pctx) {
		// A session/cancel reached this prompt while it ran, even if only
		// after the model's reply ended: ACP answers it "cancelled".
		return &PromptResult{StopReason: StopCancelled}, nil
	}
	return s.finish(a, st, l.Limits, res, err)
}

// save writes the session's history to its celeste session (ruling 11),
// so `celeste resume` shows the editor's thread. Only the running prompt
// calls it; a failure is logged.
func (s *session) save(a *Agent, msgs []tui.ChatMessage) {
	s.store.SetMessagesRaw(tui.SessionMessagesFromChat(msgs))
	if err := a.saveStore(s.store); err != nil {
		a.logf("acp: session %s: saving the history: %v", s.id, err)
	}
}

// newLoop is the prompt's loop: the chat's limits (the turn cap is
// max_tool_iterations), the editor's permission prompt as Gate and
// UserPromptSubmit hooks.
func (s *session) newLoop(a *Agent, st *promptState) *loop.Loop {
	lim := loop.DefaultLimits()
	if s.cfg.MaxToolIterations > 0 {
		lim.MaxTurns = s.cfg.MaxToolIterations
	}
	return &loop.Loop{
		Client:       s.client,
		Tools:        s.env.Registry,
		Limits:       lim,
		Gate:         s.gate(a, st),
		Compact:      s.compactor,
		CheckPrompt:  loop.HookPromptCheck(s.env.Hooks),
		SessionID:    "acp-" + s.id,
		SpillCounter: &s.spillSeq,
	}
}

// promptState is what one prompt's event pump and gate share.
type promptState struct {
	mu sync.Mutex
	// blocked is why a UserPromptSubmit hook blocked the prompt.
	blocked string
	// calls are the prompt's tool calls by ID: the gate waits until the
	// editor was sent a call's tool_call before asking about it.
	calls map[string]*callInfo
}

// callInfo is one tool call as the editor was shown it.
type callInfo struct {
	sent  chan struct{} // closed once its tool_call update was sent
	title string
	kind  string
	locs  []Location
	input map[string]any
}

func newPromptState() *promptState { return &promptState{calls: map[string]*callInfo{}} }

// call returns the call's entry, creating it.
func (st *promptState) call(id string) *callInfo {
	st.mu.Lock()
	defer st.mu.Unlock()
	ci := st.calls[id]
	if ci == nil {
		ci = &callInfo{sent: make(chan struct{})}
		st.calls[id] = ci
	}
	return ci
}

// shown records that the editor was sent the call's tool_call.
func (st *promptState) shown(id string, info callInfo) {
	ci := st.call(id)
	st.mu.Lock()
	select {
	case <-ci.sent: // a repeated ID: keep the first
	default:
		ci.title, ci.kind, ci.locs, ci.input = info.title, info.kind, info.locs, info.input
		close(ci.sent)
	}
	st.mu.Unlock()
}

// update sends one session/update to the editor.
func (s *session) update(a *Agent, u any) {
	if err := a.notify("session/update", SessionNotification{SessionID: s.id, Update: u}); err != nil {
		a.logf("acp: session %s: sending an update: %v", s.id, err)
	}
}

// onEvent maps one loop event to session updates (ruling 6), on the pump.
func (s *session) onEvent(a *Agent, st *promptState, ev loop.Event) {
	switch ev.Kind {
	case loop.EventTextDelta:
		if ev.Text != "" {
			s.update(a, AgentMessageChunk(ev.Text))
		}
	case loop.EventToolStart:
		id := callID(ev.Call)
		info := callInfo{
			title: toolTitle(ev.Call.Name, ev.Call.Input),
			kind:  toolKind(ev.Call.Name),
			locs:  toolLocations(ev.Call.Input, s.cwd),
			input: ev.Call.Input,
		}
		s.update(a, ToolCall{
			SessionUpdate: UpdateToolCall,
			ToolCallID:    id,
			Title:         info.title,
			Kind:          info.kind,
			Status:        ToolStatusInProgress,
			Locations:     info.locs,
			RawInput:      info.input,
		})
		st.shown(id, info)
	case loop.EventToolResult:
		status := ToolStatusCompleted
		if ev.IsError {
			status = ToolStatusFailed
		}
		s.update(a, ToolCallUpdate{
			SessionUpdate: UpdateToolCallUpdate,
			ToolCallID:    callID(ev.Call),
			Status:        status,
			Content:       []ToolCallContent{TextToolContent(capText(ev.Text, maxToolContent))},
		})
		if ev.Call.Name == "todo" && !ev.IsError {
			s.update(a, NewPlan(todoPlan(s.cwd)))
		}
	case loop.EventCompacted, loop.EventNotice:
		if ev.Text != "" {
			s.update(a, AgentThoughtChunk(ev.Text))
		}
	case loop.EventPromptBlocked:
		st.mu.Lock()
		st.blocked = ev.Text
		st.mu.Unlock()
	}
}

// callID is the call's ID, or one made from its name when the model gave
// none.
func callID(c loop.ToolCall) string {
	if c.ID != "" {
		return c.ID
	}
	return "call_" + c.Name
}

// capText cuts s to at most max bytes on a rune boundary, saying so.
func capText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[… %d more bytes not shown]", len(s)-cut)
}

// todoPlan is the workspace's todo list as plan entries (ruling 6).
func todoPlan(workspace string) []PlanEntry {
	var entries []PlanEntry
	for _, item := range builtin.NewTodoStore(workspace).List() {
		status := ToolStatusPending
		switch item.Status {
		case "in_progress":
			status = "in_progress"
		case "done":
			status = "completed"
		}
		entries = append(entries, PlanEntry{Content: item.Title, Priority: "medium", Status: status})
	}
	return entries
}

// finish maps the run's end to a stop reason (ruling 10). A guard's or a
// blocking hook's notice is sent first as agent text.
func (s *session) finish(a *Agent, st *promptState, lim loop.Limits, res loop.Result, err error) (*PromptResult, *RPCError) {
	notice := ""
	switch res.StopReason {
	case loop.StopError:
		msg := "the model request failed"
		if err != nil {
			msg = err.Error()
		}
		return nil, &RPCError{Code: CodeInternal, Message: msg}
	case loop.StopInterrupted:
		return &PromptResult{StopReason: StopCancelled}, nil
	case loop.StopCap:
		return &PromptResult{StopReason: StopMaxTurnRequests}, nil
	case loop.StopIdentical:
		notice = fmt.Sprintf("Stopped: the model made the identical tool call %d times in a row (stuck loop). Send another message (or rephrase the goal) to continue.", lim.IdenticalCalls)
	case loop.StopProgress:
		notice = fmt.Sprintf("Stopped: the tools returned the same results %d turns in a row. Send another message (or rephrase the goal) to continue.", lim.NoProgressTurns)
	case loop.StopInvalidArgs:
		notice = "Stopped: the model kept sending invalid tool arguments. Send another message to continue."
	case loop.StopBlocked:
		st.mu.Lock()
		reason := st.blocked
		st.mu.Unlock()
		notice = "A UserPromptSubmit hook blocked this prompt"
		if reason != "" {
			notice += ": " + reason
		}
	}
	if notice != "" {
		s.update(a, AgentMessageChunk("\n\n"+notice))
	}
	return &PromptResult{StopReason: StopEndTurn}, nil
}

// shownWait bounds how long a permission ask waits for its call's
// tool_call update to be sent (the pump sends it just before the call runs).
const shownWait = 2 * time.Second

// gate is the prompt's permission Gate (ruling 8): it asks the editor with
// session/request_permission about the pending call. "Always allow" adds
// the tool to the session's allow set (never to permissions.json): later
// asks for it are answered without a request, except an ask a PreToolUse
// hook or an AskAdvisor forced. A reject, a cancelled
// outcome, a transport error or the prompt ending is a deny.
func (s *session) gate(a *Agent, st *promptState) loop.Gate {
	return loop.GateFunc(func(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse {
		deny := tools.PermissionResponse{Decision: "deny"}
		allow := tools.PermissionResponse{Decision: "allow_once"}
		if ctx.Err() != nil {
			return deny
		}
		// A hook's or advisor's forced ask is always asked (the TUI re-asks
		// too); the allow set only answers the policy's own asks.
		if !req.Forced && s.allowed(req.ToolName) {
			return allow
		}
		tc := ToolCallUpdate{Title: summaryTitle(req.ToolName, req.InputSummary), Kind: toolKind(req.ToolName), Status: ToolStatusPending}
		if id := tools.CallIDFromContext(ctx); id != "" {
			tc.ToolCallID = id
			ci := st.call(id)
			wait := time.NewTimer(shownWait)
			select {
			case <-ci.sent:
				st.mu.Lock()
				tc.Title, tc.Kind, tc.Locations, tc.RawInput = ci.title, ci.kind, ci.locs, ci.input
				st.mu.Unlock()
			case <-wait.C:
			case <-ctx.Done():
				wait.Stop()
				return deny
			}
			wait.Stop()
		} else {
			// No call ID to show: one of its own, unique in the session.
			tc.ToolCallID = fmt.Sprintf("perm_%s_%d", req.ToolName, s.permSeq.Add(1))
		}
		params := RequestPermissionParams{
			SessionID: s.id,
			ToolCall:  tc,
			Options: []PermissionOption{
				{OptionID: OptionAllowOnce, Name: "Allow", Kind: OptionAllowOnce},
				{OptionID: OptionAllowAlways, Name: "Always allow " + req.ToolName, Kind: OptionAllowAlways},
				{OptionID: OptionRejectOnce, Name: "Reject", Kind: OptionRejectOnce},
			},
		}
		var out RequestPermissionResult
		if err := a.call(ctx, "session/request_permission", params, &out); err != nil {
			if ctx.Err() == nil {
				a.logf("acp: session %s: permission request for %s: %v", s.id, req.ToolName, err)
			}
			return deny
		}
		if ctx.Err() != nil || out.Outcome.Outcome != OutcomeSelected {
			return deny
		}
		switch out.Outcome.OptionID {
		case OptionAllowOnce:
			return allow
		case OptionAllowAlways:
			s.mu.Lock()
			s.allow[req.ToolName] = true
			s.mu.Unlock()
			return allow
		}
		return deny
	})
}

// allowed reports the editor's user allowed tool always in this session.
func (s *session) allowed(tool string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allow[tool]
}
