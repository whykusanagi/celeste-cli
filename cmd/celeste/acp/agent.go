package acp

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
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

	mu     sync.Mutex
	conn   *Conn
	closed bool
}

// NewAgent returns an agent; Attach gives it the connection it answers on.
func NewAgent(deps Deps) *Agent {
	return &Agent{deps: deps}
}

// Attach sets the connection the agent sends its requests and
// notifications on.
func (a *Agent) Attach(c *Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conn = c
}

// Close shuts the agent down: later requests are answered with an
// internal error.
func (a *Agent) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
}

func (a *Agent) logf(format string, args ...any) {
	if a.deps.Logf != nil {
		a.deps.Logf(format, args...)
	}
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
	}
	return nil, &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + method}
}

// Notify implements Handler.
func (a *Agent) Notify(method string, _ json.RawMessage) {
	a.logf("acp: ignoring notification %s", method)
}

// initialize answers with the agent's latest protocol version whatever the
// client asked for (the client disconnects if it cannot speak it), and the
// capabilities of ruling 5: embedded context yes, images and audio no.
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
