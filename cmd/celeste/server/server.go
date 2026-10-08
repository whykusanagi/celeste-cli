// Package server implements a Model Context Protocol (MCP) server that exposes
// Celeste's capabilities to external clients such as Claude Code, Codex, or any
// MCP-compatible tool orchestrator.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

const (
	serverName    = "celeste"
	serverVersion = "1.16.0" // x-release-please-version
)

// Config holds MCP server configuration.
type Config struct {
	// Transport mode: "stdio" or "sse"
	Transport string

	// SSE-specific settings
	Port      int
	BindAddr  string // default "127.0.0.1"
	Remote    bool   // if true, bind to BindAddr (possibly 0.0.0.0)
	CertFile  string // TLS certificate (required with Remote)
	KeyFile   string // TLS private key (required with Remote)
	TokenFile string // path to bearer token file (default ~/.celeste/server.token)
	RateLimit int    // requests per minute (default 60)

	// Celeste config for creating LLM clients
	CelesteConfig *config.Config
	Workspace     string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Transport: "stdio",
		Port:      8420,
		BindAddr:  "127.0.0.1",
		RateLimit: 60,
	}
}

// ToolHandler processes a tool call and returns content blocks.
type ToolHandler func(ctx context.Context, args map[string]any) ([]mcp.ContentBlock, error)

// ContentBlock is a content item in a tool call response.
// Re-exported from the mcp package for handler convenience.
type ContentBlock = mcp.ContentBlock

// Server is the MCP server that exposes Celeste tools to external clients.
type Server struct {
	config   Config
	tools    []mcp.MCPToolDef
	handlers map[string]ToolHandler
	mu       sync.RWMutex
	done     chan struct{}

	// indexers caches one *codegraph.Indexer per workspace path so
	// direct-query MCP tools (celeste_code_search, celeste_code_review,
	// ...) don't re-open the SQLite store on every call. Lazily
	// populated on first use via indexerFor, capped at maxIndexers with
	// least-recently-used eviction, and released on Close. Each call holds
	// its entry between indexerFor and release, and an entry taken out of
	// the cache closes only when its last call releases it.
	indexerMu  sync.Mutex
	indexers   map[string]*indexerEntry
	indexerSeq uint64 // use counter for LRU order (guarded by indexerMu)
	// rebuilding marks the workspaces a celeste_index rebuild is
	// rebuilding (guarded by indexerMu). indexerFor refuses to open their
	// index until the rebuild has cached the rebuilt Indexer, so no call
	// is left on the database the rebuild deletes (review of #393).
	rebuilding map[string]bool
	// retiredBusy holds, per workspace, the entries taken out of the cache
	// (by eviction, a rebuild or Close) while calls still use them, until
	// they close (guarded by indexerMu). A rebuild waits for these as well
	// as the cached entry, so it never deletes a database a call evicted
	// from the cache is still reading (Aikido 806869451).
	retiredBusy map[string][]*indexerEntry

	// runs tracks MCP agent runs that outlived the inline threshold, so a client
	// can poll for a result instead of holding an HTTP call open for minutes.
	// In-memory: a run dies with the server when the client restarts.
	runMu sync.Mutex
	runs  map[string]*BackgroundRun

	// chatEnvs caches MCP chat's loop.Env per workspace (2.0 F2b). Released
	// on Close.
	chatEnvs *chatEnvs

	// cost adds up this process's LLM usage for celeste_status (#210).
	cost sessionCost
	// health counts completion outcomes for celeste_status (2.0 W3).
	health completionHealth

	// modelNotes holds the model-resolution notes already logged.
	modelNotes sync.Map
}

// New creates a new MCP server with the given configuration.
func New(cfg Config) *Server {
	s := &Server{
		config:      cfg,
		handlers:    make(map[string]ToolHandler),
		done:        make(chan struct{}),
		indexers:    make(map[string]*indexerEntry),
		rebuilding:  make(map[string]bool),
		retiredBusy: make(map[string][]*indexerEntry),
		runs:        make(map[string]*BackgroundRun),
		chatEnvs:    newChatEnvs(),
	}
	return s
}

// maxIndexers bounds the indexer cache: the workspace argument can vary
// per call, and each entry holds a SQLite store open (Aikido 806869934).
const maxIndexers = 8

// indexerEntry is one cached Indexer and the calls using it.
type indexerEntry struct {
	idx     *codegraph.Indexer
	path    string // the workspace it is cached under
	inUse   int
	lastUse uint64
	retired bool          // out of the cache: closed when the last call releases it
	idle    chan struct{} // closed once a retired entry's Indexer is closed
}

// Close releases resources held by the server. Must be called on
// shutdown — the indexer cache holds SQLite connections that won't
// flush otherwise. Safe to call multiple times; second and subsequent
// calls are no-ops. An Indexer a call is still using closes when that
// call releases it.
func (s *Server) Close() error {
	if s.chatEnvs != nil {
		s.chatEnvs.close()
	}
	s.indexerMu.Lock()
	var idle []*indexerEntry
	for path, e := range s.indexers {
		if s.retireIndexerLocked(path, e) {
			idle = append(idle, e)
		}
	}
	s.indexerMu.Unlock()
	closeIndexerEntries(idle)
	return nil
}

// retireIndexerLocked takes e out of the cache. It reports whether e is
// idle, in which case the caller must close it (closeIndexerEntries) after
// releasing indexerMu; a busy entry closes on its last release.
func (s *Server) retireIndexerLocked(path string, e *indexerEntry) bool {
	if s.indexers[path] == e {
		delete(s.indexers, path)
	}
	if e.retired {
		return false
	}
	e.retired = true
	e.path = path
	if e.inUse > 0 {
		s.retiredBusy[path] = append(s.retiredBusy[path], e)
		return false
	}
	return true
}

// dropRetiredBusyLocked forgets e once its last call released it.
func (s *Server) dropRetiredBusyLocked(e *indexerEntry) {
	list := s.retiredBusy[e.path]
	for i, r := range list {
		if r == e {
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(s.retiredBusy, e.path)
	} else {
		s.retiredBusy[e.path] = list
	}
}

func closeIndexerEntries(entries []*indexerEntry) {
	for _, e := range entries {
		if e.idx != nil {
			_ = e.idx.Close()
		}
		close(e.idle)
	}
}

// releaseIndexer ends one call's use of e.
func (s *Server) releaseIndexer(e *indexerEntry) {
	s.indexerMu.Lock()
	e.inUse--
	closeNow := e.retired && e.inUse == 0
	if closeNow {
		s.dropRetiredBusyLocked(e)
	}
	s.indexerMu.Unlock()
	if closeNow {
		closeIndexerEntries([]*indexerEntry{e})
	}
}

// acquireIndexerLocked marks one more call on e and returns its release.
func (s *Server) acquireIndexerLocked(e *indexerEntry) func() {
	e.inUse++
	s.indexerSeq++
	e.lastUse = s.indexerSeq
	var once sync.Once
	return func() { once.Do(func() { s.releaseIndexer(e) }) }
}

// evictIndexersLocked makes room for one more entry, retiring the least
// recently used ones (idle ones first). It returns the idle entries the
// caller must close after releasing indexerMu.
func (s *Server) evictIndexersLocked() []*indexerEntry {
	var idle []*indexerEntry
	for len(s.indexers) >= maxIndexers {
		var victimPath string
		var victim *indexerEntry
		for path, e := range s.indexers {
			if victim == nil || evictBefore(e, victim) {
				victimPath, victim = path, e
			}
		}
		if s.retireIndexerLocked(victimPath, victim) {
			idle = append(idle, victim)
		}
	}
	return idle
}

// evictBefore orders eviction: idle entries before busy ones, then the
// least recently used first.
func evictBefore(a, b *indexerEntry) bool {
	if (a.inUse == 0) != (b.inUse == 0) {
		return a.inUse == 0
	}
	return a.lastUse < b.lastUse
}

// indexerFor returns the cached *codegraph.Indexer for the given
// workspace, opening a new one if none is cached. Opening is lazy
// and non-destructive: it does NOT auto-build the index — callers
// that want a fresh index must invoke the celeste_index tool with
// operation="rebuild" or "update". If no codegraph.db exists for the
// workspace yet, the returned indexer will be backed by an empty
// store and queries will return empty results until the first index
// is built.
//
// While a celeste_index rebuild of the workspace runs it fails with
// indexBuildingError instead of opening the database the rebuild deletes.
//
// The caller must call release once it is done with the Indexer: until
// then a rebuild or an eviction does not close it under the call
// (Aikido 806869451).
//
// The bool return is true when the indexer already existed in the
// cache (cache hit) and false when we just opened it (cache miss).
// Tests use the flag; callers normally ignore it.
func (s *Server) indexerFor(workspace string) (idx *codegraph.Indexer, release func(), cached bool, err error) {
	if workspace == "" {
		workspace = s.config.Workspace
	}
	s.indexerMu.Lock()
	if s.rebuilding[workspace] {
		s.indexerMu.Unlock()
		return nil, nil, false, indexBuildingError(workspace)
	}
	if e, ok := s.indexers[workspace]; ok {
		release = s.acquireIndexerLocked(e)
		s.indexerMu.Unlock()
		return e.idx, release, true, nil
	}
	dbPath := codegraph.DefaultIndexPath(workspace)
	opened, err := codegraph.NewIndexer(workspace, dbPath)
	if err != nil {
		s.indexerMu.Unlock()
		return nil, nil, false, fmt.Errorf("open indexer for %s: %w", workspace, err)
	}
	idle := s.evictIndexersLocked()
	e := &indexerEntry{idx: opened, idle: make(chan struct{})}
	s.indexers[workspace] = e
	release = s.acquireIndexerLocked(e)
	s.indexerMu.Unlock()
	closeIndexerEntries(idle)
	return opened, release, false, nil
}

// indexerCached reports whether workspace has an Indexer in the cache.
func (s *Server) indexerCached(workspace string) bool {
	s.indexerMu.Lock()
	defer s.indexerMu.Unlock()
	_, ok := s.indexers[workspace]
	return ok
}

// toolError is a soft tool failure: the call reached the tool, but the
// tool could not answer it (missing or invalid arguments, no index, an
// unknown run). handleCallTool returns its message verbatim in a result
// with isError set, so the client can tell it from a successful answer
// (#399). JSON-RPC errors stay reserved for protocol faults.
type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }

// softError builds a toolError from a format string.
func softError(format string, args ...any) error {
	return &toolError{msg: fmt.Sprintf(format, args...)}
}

// RegisterTool adds a tool definition and its handler to the server.
func (s *Server) RegisterTool(def mcp.MCPToolDef, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, def)
	s.handlers[def.Name] = handler
}

// Serve starts the MCP server using the configured transport.
// It blocks until the context is cancelled or the server is shut down.
func (s *Server) Serve(ctx context.Context) error {
	switch s.config.Transport {
	case "stdio":
		return s.serveStdio(ctx)
	case "sse":
		return s.serveSSE(ctx)
	default:
		return fmt.Errorf("unknown transport: %s", s.config.Transport)
	}
}

// handleInitialize processes the MCP initialize handshake.
func (s *Server) handleInitialize(req *mcp.Request) (*mcp.Response, error) {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal initialize result: %w", err)
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      json.Number(fmt.Sprintf("%d", req.ID)),
		Result:  resultJSON,
	}, nil
}

// handleListTools returns all registered tool definitions.
func (s *Server) handleListTools(req *mcp.Request) (*mcp.Response, error) {
	s.mu.RLock()
	toolsCopy := make([]mcp.MCPToolDef, len(s.tools))
	copy(toolsCopy, s.tools)
	s.mu.RUnlock()

	result := map[string]any{
		"tools": toolsCopy,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tools/list result: %w", err)
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      json.Number(fmt.Sprintf("%d", req.ID)),
		Result:  resultJSON,
	}, nil
}

// handleCallTool dispatches a tool call to the registered handler.
func (s *Server) handleCallTool(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, -32602, "invalid params", nil), nil
	}

	s.mu.RLock()
	handler, ok := s.handlers[params.Name]
	s.mu.RUnlock()

	if !ok {
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("tool not found: %s", params.Name), nil), nil
	}

	content, err := handler(ctx, params.Arguments)
	if err != nil {
		// Tool execution error -- return as tool result with isError, not
		// JSON-RPC error. A toolError carries a message meant for the
		// caller as is; anything else is prefixed "Error: ".
		text := fmt.Sprintf("Error: %v", err)
		var te *toolError
		if errors.As(err, &te) {
			text = te.msg
		}
		errContent := []ContentBlock{{Type: "text", Text: text}}
		result := map[string]any{
			"content": errContent,
			"isError": true,
		}
		resultJSON, _ := json.Marshal(result)
		return &mcp.Response{
			JSONRPC: "2.0",
			ID:      json.Number(fmt.Sprintf("%d", req.ID)),
			Result:  resultJSON,
		}, nil
	}

	result := map[string]any{
		"content": content,
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call result: %w", err)
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      json.Number(fmt.Sprintf("%d", req.ID)),
		Result:  resultJSON,
	}, nil
}

// dispatch routes a JSON-RPC request to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	log.Printf("[mcp-server] <- %s (id=%d)", req.Method, req.ID)

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "notifications/initialized":
		// Notification -- no response required
		return nil, nil
	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method), nil), nil
	}
}

// errorResponse creates a JSON-RPC error response.
func (s *Server) errorResponse(id int64, code int, message string, data any) *mcp.Response {
	errObj := &mcp.ErrorObject{
		Code:    code,
		Message: message,
	}
	if data != nil {
		d, _ := json.Marshal(data)
		errObj.Data = d
	}
	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      json.Number(fmt.Sprintf("%d", id)),
		Error:   errObj,
	}
}
