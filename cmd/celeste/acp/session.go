package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
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
