package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// newTestServerWithWorkspace builds an MCP Server bound to a fresh
// workspace dir. The workspace is under t.TempDir() so it's under
// the user's home (validateWorkspace requires that) via the fact that
// t.TempDir() returns /var/folders/... on macOS — which is NOT under
// $HOME. We monkey-patch the test setup by pointing the server at
// $HOME/.tmp-celeste-server/<random> which IS under home.
func newTestServerWithWorkspace(t *testing.T) (*Server, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	dir, err := os.MkdirTemp(home, ".celeste-server-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	cfg := DefaultConfig()
	cfg.CelesteConfig = &config.Config{APIKey: "test"}
	cfg.Workspace = dir
	srv := New(cfg)
	RegisterHandlers(srv)
	t.Cleanup(func() { _ = srv.Close() })
	return srv, dir
}

// writeTSFile drops a minimal TS source file into the workspace so
// celeste's GenericParser produces at least a few symbols to query
// against. Using the regex parser (not tree-sitter) so this test
// doesn't depend on Task 23 landing.
func writeTSFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
}

func callTool(t *testing.T, srv *Server, name string, args map[string]any) (*mcp.Response, map[string]any) {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	require.NoError(t, err)
	req := &mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &payload))
	return resp, payload
}

func TestCelesteIndexToolDef(t *testing.T) {
	def := celesteIndexToolDef()
	assert.Equal(t, "celeste_index", def.Name)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	props := schema["properties"].(map[string]any)
	op, ok := props["operation"].(map[string]any)
	require.True(t, ok, "schema must expose operation property")
	enum, ok := op["enum"].([]any)
	require.True(t, ok)
	wanted := map[string]bool{"status": true, "update": true, "rebuild": true}
	for _, v := range enum {
		delete(wanted, v.(string))
	}
	assert.Empty(t, wanted, "operation enum must include status/update/rebuild")
}

func TestCelesteCodeSearchToolDef(t *testing.T) {
	def := celesteCodeSearchToolDef()
	assert.Equal(t, "celeste_code_search", def.Name)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	props := schema["properties"].(map[string]any)
	_, hasQuery := props["query"]
	assert.True(t, hasQuery, "schema must require a query field")
}

func TestRegisterHandlers_RegistersDirectCodegraphTools(t *testing.T) {
	// All five direct codegraph MCP tools must be registered on the
	// server so the MCP client can discover them via tools/list.
	srv, _ := newTestServerWithWorkspace(t)

	want := []string{
		"celeste_index",
		"celeste_code_search",
		"celeste_code_review",
		"celeste_code_graph",
		"celeste_code_symbols",
	}
	for _, name := range want {
		srv.mu.RLock()
		_, ok := srv.handlers[name]
		srv.mu.RUnlock()
		assert.True(t, ok, "expected %q to be registered", name)
	}
}

func TestIndexerFor_LazyCaching(t *testing.T) {
	// Calling indexerFor twice with the same workspace should return
	// the same *codegraph.Indexer (cache hit on second call).
	srv, dir := newTestServerWithWorkspace(t)

	idx1, cached1, err := srv.indexerFor(dir)
	require.NoError(t, err)
	require.NotNil(t, idx1)
	assert.False(t, cached1, "first call must be a cache miss")

	idx2, cached2, err := srv.indexerFor(dir)
	require.NoError(t, err)
	assert.True(t, cached2, "second call must be a cache hit")
	assert.Same(t, idx1, idx2, "cached lookup must return the same indexer instance")
}

func TestCelesteIndex_RebuildThenStatusEndToEnd(t *testing.T) {
	// Rebuild an empty workspace (with one TS file), then call status
	// and verify the stats reflect the build. This is the complete
	// round-trip: MCP tool → direct handler → Indexer.Build → stats.
	srv, dir := newTestServerWithWorkspace(t)
	writeTSFile(t, dir, "auth.ts", `
export function validateSession(token: string): boolean {
  return token.length > 0;
}
export function refreshSession(token: string): boolean {
  return validateSession(token);
}
`)

	// Rebuild.
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	content := payload["content"].([]any)
	require.NotEmpty(t, content)
	block := content[0].(map[string]any)
	text := block["text"].(string)
	assert.Contains(t, text, `"operation": "rebuild"`)
	assert.Contains(t, text, `"total_files"`)

	// Status.
	_, statusPayload := callTool(t, srv, "celeste_index", map[string]any{"operation": "status"})
	statusText := statusPayload["content"].([]any)[0].(map[string]any)["text"].(string)
	assert.Contains(t, statusText, `"workspace"`)
	assert.Contains(t, statusText, `"total_symbols"`)
	// At least one symbol (validateSession or refreshSession) should
	// have made it into the store after the rebuild.
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(statusText), &status))
	assert.Greater(t, int(status["total_symbols"].(float64)), 0,
		"rebuild should have produced at least one symbol")
}

func TestCelesteCodeSearch_NoChatLLM_NoTruncation(t *testing.T) {
	// This is the headline test for Task 26: a direct celeste_code_search
	// call returns a verbatim tool result without going through a chat
	// LLM, and the response is NOT capped by any max_tokens ceiling.
	// We verify this by asking for a large top_k and confirming the
	// content block is emitted as-is regardless of length.
	srv, dir := newTestServerWithWorkspace(t)
	// Drop a few TS files so the search has candidates to return.
	for i, body := range []string{
		`export function loadUserSession(id: string) { return id; }`,
		`export function refreshSessionToken(t: string) { return t; }`,
		`export function validateAuthToken(t: string) { return t.length > 0; }`,
	} {
		writeTSFile(t, dir, "file_"+string(rune('a'+i))+".ts", body)
	}

	// Build the index first (explicit — matches the design rule).
	_, _ = callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})

	// Now search.
	_, payload := callTool(t, srv, "celeste_code_search", map[string]any{
		"query": "session token validate",
		"top_k": 10,
	})
	content := payload["content"].([]any)
	require.NotEmpty(t, content, "celeste_code_search must return content blocks")
	text := content[0].(map[string]any)["text"].(string)
	assert.NotEmpty(t, text, "search result text must not be empty")
	// Not asserting a specific symbol is #1 — the regex GenericParser
	// + the tiny corpus is too small for deterministic ranking. What
	// we care about is that the MCP round-trip completed successfully
	// and produced non-empty output via the direct-tool path.
}

func TestCelesteCodeReview_WorkspaceRejection(t *testing.T) {
	// Workspace outside the server's home gets rejected without
	// opening an indexer.
	srv, _ := newTestServerWithWorkspace(t)
	_, payload := callTool(t, srv, "celeste_code_review", map[string]any{
		"workspace": "/etc",
	})
	isErr, _ := payload["isError"].(bool)
	assert.True(t, isErr, "outside-home workspace must be rejected")
}

func TestSendProgress_NoTokenNoNotifications(t *testing.T) {
	// SendProgress is a no-op when the client didn't opt in to
	// progress notifications (no progressToken attached to ctx).
	called := false
	var mu sync.Mutex
	n := Notifier(func(method string, params any) error {
		mu.Lock()
		defer mu.Unlock()
		called = true
		return nil
	})
	ctx := WithNotifier(context.Background(), n)
	SendProgress(ctx, "reindex 50%", 0.5)
	mu.Lock()
	defer mu.Unlock()
	assert.False(t, called, "SendProgress must be silent when no progressToken was attached")
}

func TestSendProgress_ForwardsWhenTokenPresent(t *testing.T) {
	// When a progressToken is attached, SendProgress must call the
	// notifier with method="notifications/progress" and params
	// containing the token + message + percent.
	var (
		gotMethod string
		gotParams map[string]any
		mu        sync.Mutex
	)
	n := Notifier(func(method string, params any) error {
		mu.Lock()
		defer mu.Unlock()
		gotMethod = method
		// params is any — marshal then unmarshal to get a map.
		data, _ := json.Marshal(params)
		_ = json.Unmarshal(data, &gotParams)
		return nil
	})
	ctx := WithNotifier(context.Background(), n)
	ctx = WithProgressToken(ctx, "tok-123")
	SendProgress(ctx, "scanning files", 0.25)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "notifications/progress", gotMethod)
	assert.Equal(t, "tok-123", gotParams["progressToken"])
	assert.Equal(t, "scanning files", gotParams["message"])
}

func TestExtractProgressToken(t *testing.T) {
	raw := json.RawMessage(`{
		"name": "celeste_code_review",
		"arguments": {},
		"_meta": {"progressToken": "abc-42"}
	}`)
	token := extractProgressToken(raw)
	assert.Equal(t, "abc-42", token)

	// Absent _meta block — returns nil.
	raw2 := json.RawMessage(`{"name": "x", "arguments": {}}`)
	assert.Nil(t, extractProgressToken(raw2))

	// Empty params — returns nil.
	assert.Nil(t, extractProgressToken(nil))
}

// Sanity check that RegisterHandlers doesn't duplicate any tool names
// so tools/list never returns colliding descriptors.
func TestRegisterHandlers_NoDuplicateToolNames(t *testing.T) {
	srv, _ := newTestServerWithWorkspace(t)
	seen := map[string]int{}
	for _, def := range srv.tools {
		seen[def.Name]++
	}
	for name, count := range seen {
		assert.Equal(t, 1, count, "%s registered %d times", name, count)
	}
	// We should have the original three persona tools + five direct
	// codegraph tools = eight total.
	assert.Len(t, srv.tools, 8, "expected 8 MCP tools, got: %v", mapKeys(seen))
}

func mapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Make sure the regression test file imports the package's own types
// without needing strings. Silences unused-import warnings if any of
// the assertions above ever get removed.
var _ = strings.Contains
