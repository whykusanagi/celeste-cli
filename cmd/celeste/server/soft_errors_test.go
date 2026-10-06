package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payloadText returns the first content block's text of a tools/call result.
func payloadText(t *testing.T, payload map[string]any) string {
	t.Helper()
	content, ok := payload["content"].([]any)
	require.True(t, ok, "result has no content: %v", payload)
	require.NotEmpty(t, content)
	return content[0].(map[string]any)["text"].(string)
}

// #399: a soft error from a codegraph builtin (here a missing required
// argument) reaches the MCP client with isError set and the tool's own
// message, not as a plain successful result.
func TestDirectTools_SoftErrorSetsIsError(t *testing.T) {
	srv, dir := newTestServerWithWorkspace(t)
	writeTSFile(t, dir, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"celeste_code_graph", map[string]any{}, "symbol"},
		{"celeste_code_search", map[string]any{}, "query"},
		{"celeste_code_symbols", map[string]any{}, "file"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			resp, payload := callTool(t, srv, tc.tool, tc.args)
			require.Nil(t, resp.Error)
			assert.Equal(t, true, payload["isError"], "soft error must set isError")
			text := payloadText(t, payload)
			assert.Contains(t, text, tc.want)
			assert.NotContains(t, text, "Error: Error:")
		})
	}
}

// #399 audit: celeste_status with an unknown run_id is an error result.
func TestCelesteStatus_UnknownRunIsError(t *testing.T) {
	srv, _ := newTestServerWithWorkspace(t)
	resp, payload := callTool(t, srv, "celeste_status", map[string]any{"run_id": "bg-nope"})
	require.Nil(t, resp.Error)
	assert.Equal(t, true, payload["isError"])
	assert.Contains(t, payloadText(t, payload), `unknown run "bg-nope"`)
}
