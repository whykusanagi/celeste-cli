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

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
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

	// running is set while a prompt runs (ruling 2: one at a time).
	running atomic.Bool

	mu sync.Mutex
	// history is the conversation; only the running prompt replaces it.
	history []tui.ChatMessage
	// cancel stops the running prompt (session/cancel); nil when idle.
	cancel context.CancelFunc
	// allow holds the tools the editor's user allowed always (ruling 8).
	allow map[string]bool
	// pendingHooks are the untrusted repo hook sources Setup skipped
	// (ruling 9); askedHooks is set once the user was asked about them.
	pendingHooks []hooks.Source
	askedHooks   bool
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
	s := &session{id: store.ID, cwd: cwd, cfg: cfg, store: store, allow: map[string]bool{}}
	if rerr := a.setupEnv(ctx, s, p.McpServers); rerr != nil {
		return nil, rerr
	}
	store.Metadata["workspace"] = cwd
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
// recorded and skipped: Approve never blocks (ruling 9).
func (a *Agent) setupEnv(ctx context.Context, s *session, servers []McpServer) *RPCError {
	warn := func(msg string) { a.logf("acp: session %s: %s", s.id, msg) }
	env, err := loop.Setup(loop.ModeChat, s.cfg, s.cwd, loop.SetupOptions{
		SessionID: s.id,
		Warn:      warn,
		Notice:    warn,
		Approve:   s.recordHook,
	})
	if err != nil {
		return &RPCError{Code: CodeInternal, Message: "setting up the session: " + err.Error()}
	}
	a.connectMCP(ctx, s.id, env, servers)
	env.RefreshDiscovery()

	llmCfg := llm.ConfigFrom(s.cfg)
	llmCfg.SimulateTyping = false // the editor streams; typing delays only slow it
	client := llm.NewClient(llmCfg, env.Registry)
	client.SetToolMode(tools.ModeChat)
	env.StartSession(ctx, "acp")
	prompt := env.SystemPrompt("", nil)
	client.SetSystemPrompt(prompt)

	s.env, s.client, s.systemPrompt = env, client, prompt
	return nil
}

// recordHook is the Env's hook approver: it never blocks, records the
// source for the first prompt to ask about, and answers no, so Setup skips
// it (F0, ruling 9).
func (s *session) recordHook(src hooks.Source, _ hooks.TrustStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pendingHooks {
		if p.Path == src.Path && p.Kind == src.Kind {
			return false
		}
	}
	s.pendingHooks = append(s.pendingHooks, src)
	return false
}

// connectMCP connects the client's stdio MCP servers (ruling 4). Failures
// are logged and the session goes on; http and sse servers (not
// advertised) are ignored.
func (a *Agent) connectMCP(ctx context.Context, sid string, env *loop.Env, servers []McpServer) {
	for _, srv := range servers {
		if srv.Type != "" && srv.Type != "stdio" {
			a.logf("acp: session %s: ignoring %s MCP server %q (only stdio is supported)", sid, srv.Type, srv.Name)
			continue
		}
		if srv.Name == "" || srv.Command == "" {
			a.logf("acp: session %s: ignoring an MCP server without a name or command", sid)
			continue
		}
		envVars := map[string]string{}
		for _, v := range srv.Env {
			envVars[v.Name] = v.Value
		}
		cctx, cancel := context.WithTimeout(ctx, mcpConnectTimeout)
		err := env.MCP.Connect(cctx, srv.Name, mcp.ServerConfig{
			Enabled: true, Transport: "stdio", Command: srv.Command, Args: srv.Args, Env: envVars,
		})
		cancel()
		if err != nil {
			a.logf("acp: session %s: MCP server %q: %v", sid, srv.Name, err)
		}
	}
}

// close releases the session's Env.
func (s *session) close() {
	if s.env != nil {
		s.env.Close()
	}
}

// cancelPrompt cancels the running prompt, if any.
func (s *session) cancelPrompt() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// maxToolContent caps the tool result text a tool_call_update carries
// (ruling 6); the model gets the whole result.
const maxToolContent = 8 << 10

// prompt runs one session/prompt on the loop (rulings 2, 6, 10): one at a
// time per session, the loop's events streamed as session/update, and the
// stop reason answered when the turn ends.
func (s *session) prompt(ctx context.Context, a *Agent, text string) (*PromptResult, *RPCError) {
	if !s.running.CompareAndSwap(false, true) {
		return nil, &RPCError{Code: CodeBusy, Message: "a prompt is already running in this session"}
	}
	defer s.running.Store(false)
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.cancel = cancel
	history := append(append([]tui.ChatMessage(nil), s.history...),
		tui.ChatMessage{Role: "user", Content: text, Timestamp: time.Now()})
	s.mu.Unlock()
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
	return s.finish(a, st, l.Limits, res, err)
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
		Client:      s.client,
		Tools:       s.env.Registry,
		Limits:      lim,
		Gate:        s.gate(a, st),
		CheckPrompt: loop.HookPromptCheck(s.env.Hooks),
		SessionID:   "acp-" + s.id,
	}
}

// promptState is what one prompt's event pump and gate share.
type promptState struct {
	mu sync.Mutex
	// blocked is why a UserPromptSubmit hook blocked the prompt.
	blocked string
}

func newPromptState() *promptState { return &promptState{} }

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
		s.update(a, ToolCall{
			SessionUpdate: UpdateToolCall,
			ToolCallID:    callID(ev.Call),
			Title:         toolTitle(ev.Call.Name, ev.Call.Input),
			Kind:          toolKind(ev.Call.Name),
			Status:        ToolStatusInProgress,
			Locations:     toolLocations(ev.Call.Input, s.cwd),
			RawInput:      ev.Call.Input,
		})
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

// gate is the prompt's permission Gate (ruling 8): it asks the editor with
// session/request_permission. Anything but an allow is a deny.
func (s *session) gate(a *Agent, st *promptState) loop.Gate {
	return loop.GateFunc(func(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse {
		deny := tools.PermissionResponse{Decision: "deny"}
		if ctx.Err() != nil {
			return deny
		}
		id := tools.CallIDFromContext(ctx)
		if id == "" {
			id = "call_" + req.ToolName
		}
		title := req.ToolName
		if req.InputSummary != "" {
			title = toolTitle(req.ToolName, map[string]any{"path": req.InputSummary})
		}
		params := RequestPermissionParams{
			SessionID: s.id,
			ToolCall:  ToolCallUpdate{ToolCallID: id, Title: title, Kind: toolKind(req.ToolName), Status: ToolStatusPending},
			Options: []PermissionOption{
				{OptionID: OptionAllowOnce, Name: "Allow", Kind: OptionAllowOnce},
				{OptionID: OptionRejectOnce, Name: "Reject", Kind: OptionRejectOnce},
			},
		}
		var out RequestPermissionResult
		if err := a.call(ctx, "session/request_permission", params, &out); err != nil {
			a.logf("acp: session %s: permission request for %s: %v", s.id, req.ToolName, err)
			return deny
		}
		if out.Outcome.Outcome == OutcomeSelected && out.Outcome.OptionID == OptionAllowOnce {
			return tools.PermissionResponse{Decision: "allow_once"}
		}
		return deny
	})
}
