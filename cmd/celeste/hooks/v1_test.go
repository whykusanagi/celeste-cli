package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipV1OnWindows: protocol v1 keeps 1.x semantics, `sh -c`, which Windows
// CI runners don't provide.
func skipV1OnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("protocol v1 hooks run through sh -c, unavailable on Windows runners")
	}
}

func v1Def(ev Event, command string) Definition {
	return Definition{Event: ev, Matcher: "*", Command: command, Timeout: 5, Protocol: ProtocolV1}
}

func TestV1ExitZeroAllows(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	res := runHook(context.Background(), v1Def(EventPreToolUse, "true"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	require.Empty(t, res.failed)
	assert.Equal(t, Allow, res.decision)
}

func TestV1NonZeroBlocksWithOutput(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	res := runHook(context.Background(), v1Def(EventPreToolUse, "echo blocked && exit 1"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	assert.Equal(t, Deny, res.decision)
	assert.Equal(t, "blocked", res.reason)
}

func TestV1PostToolUseNonZeroIsAFailureNotADecision(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	res := runHook(context.Background(), v1Def(EventPostToolUse, "echo oops; exit 2"), ws, toolPayload(ws, EventPostToolUse, "bash", nil))
	assert.Contains(t, res.failed, "exit status 2")
}

func TestV1StdoutIsNotParsedAsJSON(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	res := runHook(context.Background(), v1Def(EventPreToolUse, `echo '{"decision":"deny"}'`), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	require.Empty(t, res.failed)
	assert.Equal(t, Allow, res.decision, "v1 decides by exit code only")
}

// Review Focus 1: a v1 guard reads CELESTE_TOOL_COMMAND; when that had to
// be omitted, the guard can't judge the call, so it blocks.
func TestV1FailsClosedWhenInputOmitted(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	padded := "echo safe" + strings.Repeat(" ", 200<<10) + "; rm -rf /"
	res := runHook(context.Background(), v1Def(EventPreToolUse, `case "$CELESTE_TOOL_COMMAND" in *rm*) exit 1;; esac`), ws,
		toolPayload(ws, EventPreToolUse, "bash", map[string]any{"command": padded}))
	assert.Contains(t, res.failed, "protocol v2")
}

func TestV1TemplateVars(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	res := runHook(context.Background(), v1Def(EventPreToolUse, "echo {{tool}} {{path}} && exit 1"), ws,
		toolPayload(ws, EventPreToolUse, "write_file", map[string]any{"path": "/tmp/test.txt"}))
	assert.Equal(t, "write_file /tmp/test.txt", res.reason)
}

// #168: {{path}} and {{command}} come from model-chosen tool arguments.
func TestV1TemplateInjection(t *testing.T) {
	skipV1OnWindows(t)
	cases := map[string]struct {
		command string
		input   map[string]any
	}{
		"path semicolon":        {"echo {{path}}", map[string]any{"path": "a; touch PWNED"}},
		"path command subst":    {"echo {{path}}", map[string]any{"path": "$(touch PWNED)"}},
		"path backticks":        {"echo {{path}}", map[string]any{"path": "`touch PWNED`"}},
		"command in dquotes":    {`echo "{{command}}"`, map[string]any{"command": `"; touch PWNED; echo "`}},
		"single quote breakout": {"echo {{path}}", map[string]any{"path": "x'; touch PWNED; echo '"}},
		"nested placeholder": {"echo {{path}} {{command}}", map[string]any{
			"path":    "a {{command}} b",
			"command": "; touch PWNED;",
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			pwned := filepath.Join(ws, "pwned")
			input := map[string]any{}
			for k, v := range tc.input {
				input[k] = strings.ReplaceAll(v.(string), "PWNED", pwned)
			}
			runHook(context.Background(), v1Def(EventPreToolUse, tc.command), ws, toolPayload(ws, EventPreToolUse, "read_file", input))
			_, statErr := os.Stat(pwned)
			assert.True(t, os.IsNotExist(statErr), "hook command injection created %s", pwned)
		})
	}
}

func TestV1TemplateValuePreserved(t *testing.T) {
	skipV1OnWindows(t)
	for name, command := range map[string]string{
		"bare": "echo {{path}}; exit 1", "double quoted": `echo "{{path}}"; exit 1`, "single quoted": "echo '{{path}}'; exit 1",
	} {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			res := runHook(context.Background(), v1Def(EventPreToolUse, command), ws,
				toolPayload(ws, EventPreToolUse, "read_file", map[string]any{"path": "dir with space/it's $HOME.go"}))
			assert.Equal(t, "dir with space/it's $HOME.go", res.reason)
		})
	}
}

// Ported from TestExecutor_PayloadOnStdinAndEnv: both halves.
func TestV1PayloadOnStdinAndEnv(t *testing.T) {
	skipV1OnWindows(t)
	ws := t.TempDir()
	input := map[string]any{"path": "a; b", "command": "go test"}
	res := runHook(context.Background(), v1Def(EventPreToolUse, "cat; exit 1"), ws, toolPayload(ws, EventPreToolUse, "bash", input))
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.reason), &payload))
	assert.Equal(t, "bash", payload["tool_name"])
	assert.Equal(t, ws, payload["workspace"])

	res = runHook(context.Background(), v1Def(EventPreToolUse,
		`printf '%s|%s|%s|%s' "$CELESTE_HOOK_EVENT" "$CELESTE_TOOL_NAME" "$CELESTE_TOOL_PATH" "$CELESTE_TOOL_COMMAND"; exit 1`),
		ws, toolPayload(ws, EventPreToolUse, "bash", input))
	assert.Equal(t, "PreToolUse|bash|a; b|go test", res.reason)
}

func TestExpandTemplateVars(t *testing.T) {
	result := expandTemplateVars("cd {{workspace}} && {{tool}} {{path}} {{command}}", "/home/user/project", "bash",
		map[string]any{"path": "src/main.go", "command": "go build"})
	assert.Equal(t, "cd '/home/user/project' && 'bash' 'src/main.go' 'go build'", result)
}

func TestExpandTemplateVars_MissingInput(t *testing.T) {
	assert.Equal(t, "echo '' ''", expandTemplateVars("echo {{path}} {{command}}", "/ws", "test", nil))
}
