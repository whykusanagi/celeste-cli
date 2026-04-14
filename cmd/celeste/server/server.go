// Package server implements a Model Context Protocol (MCP) server that exposes
// Celeste's capabilities to external clients such as Claude Code, Codex, or any
// MCP-compatible tool orchestrator.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

const (
	serverName    = "celeste"
	serverVersion = "1.9.1"
)

// Config holds MCP server configuration.
type Config struct {
	// Transport mode: "stdio" or "sse"
	Transport string

	// SSE-specific settings
	Port      int
	BindAddr  string // default "127.0.0.1"
	Remote    bool   // if true, bind to BindAddr (possibly 0.0.0.0)
	CertFile  string // TLS certificate for mTLS
	KeyFile   string // TLS private key for mTLS
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
	// populated on first use via indexerFor. Released on Close.
	indexerMu sync.Mutex
	indexers  map[string]*codegraph.Indexer
}

// New creates a new MCP server with the given configuration.
func New(cfg Config) *Server {
	s := &Server{
		config:   cfg,
		handlers: make(map[string]ToolHandler),
		done:     make(chan struct{}),
		indexers: make(map[string]*codegraph.Indexer),
	}
	return s
}

// Close releases resources held by the server. Must be called on
// shutdown — the indexer cache holds SQLite connections that won't
// flush otherwise. Safe to call multiple times; second and subsequent
// calls are no-ops.
func (s *Server) Close() error {
	s.indexerMu.Lock()
	defer s.indexerMu.Unlock()
	for path, idx := range s.indexers {
		if idx != nil {
			_ = idx.Close()
		}
		delete(s.indexers, path)
	}
	return nil
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
// The bool return is true when the indexer already existed in the
// cache (cache hit) and false when we just opened it (cache miss).
// Tests use the flag; callers normally ignore it.
func (s *Server) indexerFor(workspace string) (*codegraph.Indexer, bool, error) {
	if workspace == "" {
		workspace = s.config.Workspace
	}
	s.indexerMu.Lock()
	defer s.indexerMu.Unlock()
	if idx, ok := s.indexers[workspace]; ok && idx != nil {
		return idx, true, nil
	}
	dbPath := codegraph.DefaultIndexPath(workspace)
	idx, err := codegraph.NewIndexer(workspace, dbPath)
	if err != nil {
		return nil, false, fmt.Errorf("open indexer for %s: %w", workspace, err)
	}
	s.indexers[workspace] = idx
	return idx, false, nil
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
		// Tool execution error -- return as tool result with isError, not JSON-RPC error
		errContent := []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}
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
