package acp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// Deps are what the agent needs from the celeste process.
type Deps struct {
	// Config loads celeste's configuration (the agent uses celeste's own
	// config and keys, never the editor's).
	Config func() (*config.Config, error)
	// Sessions is the session store ACP sessions map onto.
	Sessions *config.SessionManager
	// Home is the user's home directory (hooks trust store).
	Home string
	// Logf logs to the log file; never to stdout, which is the protocol.
	Logf func(format string, args ...any)
}

// Agent is celeste's ACP agent: the Handler of a Conn.
type Agent struct {
	deps Deps

	mu       sync.Mutex
	conn     *Conn
	closed   bool
	sessions map[string]*session
	// prompts counts running prompts; Close waits for them.
	prompts sync.WaitGroup

	// storeMu serializes the session store: SessionManager is not safe for
	// concurrent use, and requests run on their own goroutines.
	storeMu sync.Mutex
}

// NewAgent returns an agent; Attach gives it the connection it answers on.
func NewAgent(deps Deps) *Agent {
	return &Agent{deps: deps, sessions: map[string]*session{}}
}

// Attach sets the connection the agent sends its requests and
// notifications on.
func (a *Agent) Attach(c *Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conn = c
}

// closeWait bounds how long Close waits for cancelled prompts to finish.
const closeWait = 10 * time.Second

// Close shuts the agent down: later requests are answered with an internal
// error, running prompts are cancelled (and waited for, briefly), and every
// session's Env is closed. Nothing is logged once it returns.
func (a *Agent) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	sessions := a.sessions
	a.sessions = map[string]*session{}
	a.mu.Unlock()
	for _, s := range sessions {
		s.cancelPrompt()
	}
	done := make(chan struct{})
	go func() { a.prompts.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
	for _, s := range sessions {
		s.close()
	}
}

func (a *Agent) logf(format string, args ...any) {
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if a.deps.Logf != nil && !closed {
		a.deps.Logf(format, args...)
	}
}

// loadConfig is celeste's config (Deps.Config).
func (a *Agent) loadConfig() (*config.Config, error) {
	if a.deps.Config == nil {
		return nil, errors.New("no config loader")
	}
	cfg, err := a.deps.Config()
	if err == nil && cfg == nil {
		err = errors.New("no config")
	}
	return cfg, err
}

// newStore starts a celeste session record (its ID is the sessionId,
// ruling 3).
func (a *Agent) newStore() *config.Session {
	a.storeMu.Lock()
	defer a.storeMu.Unlock()
	return a.deps.Sessions.NewSession()
}

// loadStore reads a session record; an invalid ID is an error.
func (a *Agent) loadStore(id string) (*config.Session, error) {
	a.storeMu.Lock()
	defer a.storeMu.Unlock()
	return a.deps.Sessions.Load(id)
}

// saveStore writes a session record.
func (a *Agent) saveStore(s *config.Session) error {
	a.storeMu.Lock()
	defer a.storeMu.Unlock()
	return a.deps.Sessions.Save(s)
}

// addSession registers s; false once the agent is closed or when a
// session with its ID is already open.
func (a *Agent) addSession(s *session) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.sessions[s.id] != nil {
		return false
	}
	a.sessions[s.id] = s
	return true
}

// session returns the session with id, or nil.
func (a *Agent) session(id string) *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[id]
}

// Request implements Handler.
func (a *Agent) Request(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil, &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	switch method {
	case "initialize":
		var p InitializeParams
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		return a.initialize(p), nil
	case "authenticate":
		// celeste authenticates with its own config and keys; it
		// advertises no methods, so there is nothing to do.
		return struct{}{}, nil
	case "session/new":
		var p NewSessionParams
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		s, err := a.newSession(ctx, p)
		if err != nil {
			return nil, err
		}
		return NewSessionResult{SessionID: s.id}, nil
	case "session/load":
		var p LoadSessionParams
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		if err := a.loadSession(ctx, p); err != nil {
			return nil, err
		}
		// The schema's LoadSessionResponse is an object, all fields optional.
		return struct{}{}, nil
	case "session/prompt":
		var p PromptParams
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		return a.prompt(ctx, p)
	}
	return nil, &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + method}
}

// Notify implements Handler. It runs on the read loop and never blocks.
func (a *Agent) Notify(method string, params json.RawMessage) {
	switch method {
	case "session/cancel":
		var p CancelParams
		if err := json.Unmarshal(params, &p); err != nil {
			a.logf("acp: session/cancel with invalid params: %v", err)
			return
		}
		if s := a.session(p.SessionID); s != nil {
			s.cancelPrompt()
		}
	default:
		a.logf("acp: ignoring notification %s", method)
	}
}

// Received implements Receiver: a session/prompt is counted as arrived
// before its goroutine starts, so a session/cancel read right after it
// cancels it even if it has not started yet.
func (a *Agent) Received(method string, params json.RawMessage) {
	if method != "session/prompt" {
		return
	}
	var p PromptParams
	if json.Unmarshal(params, &p) != nil { // Request answers the error
		return
	}
	if s := a.session(p.SessionID); s != nil {
		s.arrived()
	}
}

// prompt answers session/prompt.
func (a *Agent) prompt(ctx context.Context, p PromptParams) (any, *RPCError) {
	s := a.session(p.SessionID)
	if s == nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "unknown session " + p.SessionID}
	}
	text, err := promptText(p.Prompt, a.logf)
	if err != nil {
		s.dequeue()
		return nil, &RPCError{Code: CodeInvalidParams, Message: err.Error()}
	}
	if !a.hold() {
		s.dequeue()
		return nil, &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	defer a.prompts.Done()
	res, rerr := s.prompt(ctx, a, text)
	if rerr != nil {
		return nil, rerr
	}
	return res, nil
}

// hold counts work on a session that Close must wait for (a prompt, or a
// load rebuilding an open session's Env); false once the agent is closed.
// The caller calls a.prompts.Done when it ends.
func (a *Agent) hold() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.prompts.Add(1)
	return true
}

// connection is the attached Conn, or nil.
func (a *Agent) connection() *Conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn
}

// notify sends a notification to the editor.
func (a *Agent) notify(method string, params any) error {
	c := a.connection()
	if c == nil {
		return errors.New("acp: no connection")
	}
	return c.Notify(method, params)
}

// call sends a request to the editor and waits for its answer.
func (a *Agent) call(ctx context.Context, method string, params, result any) error {
	c := a.connection()
	if c == nil {
		return errors.New("acp: no connection")
	}
	return c.Call(ctx, method, params, result)
}

// initialize answers with the agent's latest protocol version whatever the
// client asked for (the client disconnects if it cannot speak it), and the
// capabilities of ruling 5: embedded context yes, images and audio no;
// session/load is served (ruling 11).
func (a *Agent) initialize(p InitializeParams) InitializeResult {
	if p.ProtocolVersion != ProtocolVersion {
		a.logf("acp: client asked for protocol version %d; answering %d", p.ProtocolVersion, ProtocolVersion)
	}
	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession:        true,
			PromptCapabilities: PromptCapabilities{EmbeddedContext: true},
		},
		AuthMethods: []AuthMethod{},
	}
}

// decodeParams unmarshals request params; bad params are -32602.
func decodeParams(params json.RawMessage, v any) *RPCError {
	if len(params) == 0 {
		return &RPCError{Code: CodeInvalidParams, Message: "missing params"}
	}
	if err := json.Unmarshal(params, v); err != nil {
		return &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	return nil
}
