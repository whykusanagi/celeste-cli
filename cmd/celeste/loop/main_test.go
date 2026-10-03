package loop

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

// mcpStubEnv makes the test binary a stdio MCP server (serveStubMCP). Test
// MCP configs run os.Executable() directly, with no shell, so this works on
// Windows too.
const mcpStubEnv = "LOOP_MCP_STUB"

func TestMain(m *testing.M) {
	// Before the hook helper: a hook test's HOOKTEST_HELPER=1 would be
	// inherited by an MCP server it starts.
	if os.Getenv(mcpStubEnv) == "1" {
		os.Exit(serveStubMCP(os.Stdin, os.Stdout))
	}
	hooktest.RunIfHelper() // the test binary doubles as the hook command (F0)
	os.Exit(m.Run())
}

// serveStubMCP answers initialize, tools/list (one tool, "echo") and
// tools/call over newline-delimited JSON-RPC until stdin closes.
func serveStubMCP(in io.Reader, out io.Writer) int {
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == nil {
			continue // notifications need no answer
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "stub", "version": "1"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "echo", "description": "echo", "inputSchema": map[string]any{"type": "object"},
			}}}
		default:
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}); err != nil {
			return 1
		}
	}
	return 0
}
