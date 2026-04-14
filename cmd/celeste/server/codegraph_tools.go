// Direct codegraph MCP tools.
//
// Before this file existed, the MCP bridge to celeste's codegraph went
// through the celeste chat tool: the caller sent a natural-language
// prompt, a chat LLM decided to call code_review (or code_search, ...)
// as a function-call, the tool ran against the graph, and the chat LLM
// summarized the result back to the caller — with all the usual LLM
// costs: latency, token spend, and an output ceiling that truncated
// large findings mid-response.
//
// The tools here skip the chat LLM entirely. Each MCP tool is a direct
// handler that pulls the per-workspace Indexer from the Server cache,
// invokes the underlying celeste codegraph tool, and returns the tool
// result verbatim as an MCP ContentBlock. No model in the middle, no
// output ceiling — the client receives exactly what celeste computed.
//
// Indexing is explicit: none of these tools auto-reindex. If the
// caller's workspace has changed and the cached graph is stale, the
// caller must first call celeste_index with operation="update" (or
// "rebuild" for a full re-scan). This matches the user's mental model
// — "index once, serve the graph for many queries" — and avoids the
// silent-reindex latency spike that plagued the old chat-routed path.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// registerCodegraphTools adds the direct codegraph MCP tools to the
// server. Called from RegisterHandlers after the persona tools are
// registered.
func registerCodegraphTools(s *Server) {
	s.RegisterTool(celesteIndexToolDef(), s.handleCelesteIndex)
	s.RegisterTool(celesteCodeSearchToolDef(), s.makeDirectToolHandler(
		"celeste_code_search",
		func(idx *codegraph.Indexer) tools.Tool { return builtin.NewCodeSearchTool(idx) },
	))
	s.RegisterTool(celesteCodeReviewToolDef(), s.makeDirectToolHandler(
		"celeste_code_review",
		func(idx *codegraph.Indexer) tools.Tool { return builtin.NewCodeReviewTool(idx) },
	))
	s.RegisterTool(celesteCodeGraphToolDef(), s.makeDirectToolHandler(
		"celeste_code_graph",
		func(idx *codegraph.Indexer) tools.Tool { return builtin.NewCodeGraphTool(idx) },
	))
	s.RegisterTool(celesteCodeSymbolsToolDef(), s.makeDirectToolHandler(
		"celeste_code_symbols",
		func(idx *codegraph.Indexer) tools.Tool { return builtin.NewCodeSymbolsTool(idx) },
	))
}

// workspaceFromArgs extracts an optional "workspace" arg, falling back
// to the server's configured workspace. All direct tools accept this
// field so a single celeste serve process can answer queries against
// multiple checkouts when the client wants to.
func (s *Server) workspaceFromArgs(args map[string]any) (string, error) {
	workspace, _ := args["workspace"].(string)
	if workspace == "" {
		workspace = s.config.Workspace
	}
	if err := validateWorkspace(workspace, s.config.Workspace); err != nil {
		return "", fmt.Errorf("workspace rejected: %w", err)
	}
	return workspace, nil
}

// makeDirectToolHandler builds an MCP ToolHandler that runs a celeste
// builtin tool against the per-workspace Indexer and returns the
// result as a single text ContentBlock. All the direct-query tools
// share this plumbing — the only difference between them is which
// builtin they construct, passed as a closure so the Indexer can be
// resolved lazily per request.
func (s *Server) makeDirectToolHandler(toolName string, buildTool func(*codegraph.Indexer) tools.Tool) ToolHandler {
	return func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		workspace, err := s.workspaceFromArgs(args)
		if err != nil {
			return nil, err
		}
		// Strip the workspace arg before handing args to the tool —
		// none of the codegraph tools expect it and leaving it in
		// would fail their ValidateInput checks if they ever get one.
		delete(args, "workspace")

		idx, cached, err := s.indexerFor(workspace)
		if err != nil {
			return nil, err
		}
		if !cached {
			SendProgress(ctx, fmt.Sprintf("opened codegraph for %s", workspace), 0)
		}

		// Forward the tool's progress events onto the MCP progress
		// channel so long-running queries stream status back to the
		// caller. Buffered so the tool doesn't block if the transport
		// is slow.
		progress := make(chan tools.ProgressEvent, 16)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for evt := range progress {
				SendProgress(ctx, evt.Message, evt.Percent)
			}
		}()

		bt := buildTool(idx)
		result, execErr := bt.Execute(ctx, args, progress)
		close(progress)
		<-done

		if execErr != nil {
			return nil, fmt.Errorf("%s: %w", toolName, execErr)
		}
		if result.Error {
			// The underlying tool flagged a soft error — surface it
			// to the MCP client as an isError content block. JSON-RPC
			// level errors are reserved for plumbing faults.
			return []ContentBlock{{Type: "text", Text: result.Content}}, nil
		}
		return []ContentBlock{{Type: "text", Text: result.Content}}, nil
	}
}

// --- celeste_index ---

// celesteIndexToolDef exposes the indexer lifecycle. Keeping it as a
// single tool with an `operation` discriminator beats splitting into
// three tools because the operations share the same workspace field
// and the same progress-event shape; clients only need one descriptor
// to pin in their prompt.
func celesteIndexToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["status", "update", "rebuild"],
				"default": "status",
				"description": "status: report the current index health and stats. update: incrementally re-index only files whose content hash has changed since last run. rebuild: drop the existing index and full-scan the workspace from scratch."
			},
			"workspace": {
				"type": "string",
				"description": "Absolute workspace path (defaults to the server's cwd)."
			}
		}
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_index",
		Description: "Inspect or refresh the celeste codegraph for a workspace. Use operation=update after making code changes so subsequent celeste_code_search / celeste_code_review queries reflect the new state. This is the ONLY tool that writes to the index — the query tools never auto-reindex.",
		InputSchema: schema,
	}
}

// handleCelesteIndex dispatches to one of three operations. Rebuild and
// update stream progress events during the scan so the calling LLM can
// display per-file status instead of waiting in the dark.
func (s *Server) handleCelesteIndex(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
	workspace, err := s.workspaceFromArgs(args)
	if err != nil {
		return nil, err
	}
	op, _ := args["operation"].(string)
	if op == "" {
		op = "status"
	}

	switch op {
	case "status":
		return s.indexStatus(ctx, workspace)
	case "update":
		return s.indexUpdate(ctx, workspace)
	case "rebuild":
		return s.indexRebuild(ctx, workspace)
	default:
		return nil, fmt.Errorf("unknown operation %q (expected status, update, or rebuild)", op)
	}
}

// indexStatus reports the stored stats without mutating the graph.
// Safe to call at any time — zero cost beyond a single sqlite SELECT.
func (s *Server) indexStatus(ctx context.Context, workspace string) ([]ContentBlock, error) {
	idx, _, err := s.indexerFor(workspace)
	if err != nil {
		return nil, err
	}
	stats, err := idx.Stats()
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	report := map[string]any{
		"workspace":       workspace,
		"db_path":         codegraph.DefaultIndexPath(workspace),
		"total_files":     stats.TotalFiles,
		"total_symbols":   stats.TotalSymbols,
		"total_edges":     stats.TotalEdges,
		"symbols_by_kind": stats.SymbolsByKind,
		"files_by_lang":   stats.FilesByLang,
	}
	if bm25, _ := idx.Store().ReadBM25Stats(); bm25 != nil {
		report["bm25"] = map[string]any{
			"num_docs":       bm25.NumDocs,
			"avg_doc_length": bm25.AvgDocLength,
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return []ContentBlock{{Type: "text", Text: string(data)}}, nil
}

// indexUpdate runs an incremental reindex. Only files whose content
// hash changed since the last run are re-parsed; deleted files have
// their symbols dropped. Progress events are forwarded to the MCP
// client so the caller sees "scanning X/Y files" in real time.
func (s *Server) indexUpdate(ctx context.Context, workspace string) ([]ContentBlock, error) {
	idx, _, err := s.indexerFor(workspace)
	if err != nil {
		return nil, err
	}
	SendProgress(ctx, fmt.Sprintf("starting incremental update on %s", workspace), 0)
	start := time.Now()
	if err := idx.Update(); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	SendProgress(ctx, fmt.Sprintf("update complete in %s", elapsed), 1.0)
	stats, err := idx.Stats()
	if err != nil {
		return nil, err
	}
	report := map[string]any{
		"operation":     "update",
		"workspace":     workspace,
		"elapsed":       elapsed.String(),
		"total_files":   stats.TotalFiles,
		"total_symbols": stats.TotalSymbols,
		"total_edges":   stats.TotalEdges,
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	return []ContentBlock{{Type: "text", Text: string(data)}}, nil
}

// indexRebuild drops the cached indexer (so its db connection closes
// and WAL files can be renamed), wipes the SQLite file on disk, then
// re-opens and runs a full Build. This is the escape hatch when the
// store schema changes between celeste versions or when the index is
// suspected corrupt.
func (s *Server) indexRebuild(ctx context.Context, workspace string) ([]ContentBlock, error) {
	// Evict from cache so we hold no fds on the old file.
	s.indexerMu.Lock()
	if idx, ok := s.indexers[workspace]; ok && idx != nil {
		_ = idx.Close()
		delete(s.indexers, workspace)
	}
	s.indexerMu.Unlock()

	// Remove the existing db + WAL files so Build starts fresh.
	dbPath := codegraph.DefaultIndexPath(workspace)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = removeIfExists(dbPath + suffix)
	}

	idx, _, err := s.indexerFor(workspace)
	if err != nil {
		return nil, err
	}
	SendProgress(ctx, fmt.Sprintf("starting full rebuild on %s", workspace), 0)
	start := time.Now()
	if err := idx.Build(); err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	SendProgress(ctx, fmt.Sprintf("rebuild complete in %s", elapsed), 1.0)
	stats, err := idx.Stats()
	if err != nil {
		return nil, err
	}
	report := map[string]any{
		"operation":     "rebuild",
		"workspace":     workspace,
		"elapsed":       elapsed.String(),
		"total_files":   stats.TotalFiles,
		"total_symbols": stats.TotalSymbols,
		"total_edges":   stats.TotalEdges,
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	return []ContentBlock{{Type: "text", Text: string(data)}}, nil
}

// --- celeste_code_search ---

func celesteCodeSearchToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Natural-language or keyword description of the code to find."
			},
			"top_k": {
				"type": "integer",
				"default": 10,
				"description": "Maximum number of results to return."
			},
			"workspace": {
				"type": "string",
				"description": "Absolute workspace path (defaults to the server's cwd)."
			}
		},
		"required": ["query"]
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_code_search",
		Description: "Semantic code search over the celeste codegraph. Returns symbols matching the query ranked by MinHash Jaccard + BM25 rank fusion, with path flags and confidence warnings. Reads the cached index — call celeste_index update first if the code has changed.",
		InputSchema: schema,
	}
}

// --- celeste_code_review ---

func celesteCodeReviewToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"kinds": {
				"type": "string",
				"description": "Comma-separated categories: ALL, LAZY_REDIRECT, STUB, PLACEHOLDER, TODO_FIXME, EMPTY_HANDLER, HARDCODED (default ALL)."
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum results per category (default 30)."
			},
			"include_tests": {
				"type": "boolean",
				"description": "Include test files in the findings (default false)."
			},
			"workspace": {
				"type": "string",
				"description": "Absolute workspace path (defaults to the server's cwd)."
			}
		}
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_code_review",
		Description: "Run celeste's structural code review against the codegraph and return findings as JSON grouped by kind. No chat LLM round-trip — output is returned verbatim with no output-token ceiling. Reads the cached index.",
		InputSchema: schema,
	}
}

// --- celeste_code_graph ---

// The underlying builtin code_graph tool expects {symbol, direction, depth}
// and validates symbol as required. Mirror its schema 1:1 so the MCP
// wrapper doesn't advertise fields the tool can't accept.
func celesteCodeGraphToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"symbol": {
				"type": "string",
				"description": "Symbol name to query relationships for. Use celeste_code_search first to find it."
			},
			"direction": {
				"type": "string",
				"enum": ["callers", "callees", "both"],
				"description": "Edge direction: callers (who calls this), callees (what this calls), both. Default: both."
			},
			"depth": {
				"type": "number",
				"description": "Number of hops to traverse. Default 1, max 3."
			},
			"workspace": {
				"type": "string",
				"description": "Absolute workspace path (defaults to the server's cwd)."
			}
		},
		"required": ["symbol"]
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_code_graph",
		Description: "Query structural relationships in the celeste codegraph: who calls a function, what it calls, implements, references. Reads the cached index.",
		InputSchema: schema,
	}
}

// --- celeste_code_symbols ---

// The underlying builtin code_symbols tool expects {file, package} and
// requires at least one. Mirror the real schema — name-only lookups
// aren't supported; the dedicated semantic search tool handles those.
func celesteCodeSymbolsToolDef() mcp.MCPToolDef {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"file": {
				"type": "string",
				"description": "Relative file path to list symbols for."
			},
			"package": {
				"type": "string",
				"description": "Package name to list symbols for."
			},
			"workspace": {
				"type": "string",
				"description": "Absolute workspace path (defaults to the server's cwd)."
			}
		}
	}`)
	return mcp.MCPToolDef{
		Name:        "celeste_code_symbols",
		Description: "List all symbols in a file or package from the celeste codegraph. Provide either file or package. Reads the cached index. For name-based search, use celeste_code_search instead.",
		InputSchema: schema,
	}
}
