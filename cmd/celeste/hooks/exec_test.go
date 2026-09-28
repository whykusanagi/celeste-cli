package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func v2(t *testing.T, ev Event, args ...string) Definition {
	t.Helper()
	return Definition{Event: ev, Matcher: "*", Command: hooktest.Command(t, args...), Timeout: DefaultTimeout, Protocol: ProtocolV2}
}

func toolPayload(ws string, ev Event, tool string, input map[string]any) map[string]any {
	return map[string]any{"event": string(ev), "workspace": ws, "project_dir": ws, "session_id": "s1", "tool_name": tool, "tool_input": input}
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var env map[string]string
	require.NoError(t, json.Unmarshal(b, &env))
	return env
}

func TestV2Decisions(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		args     []string
		decision Decision
		reason   string
		context  string
	}{
		{[]string{"allow"}, Allow, "", ""},
		{[]string{"deny", "not today"}, Deny, "not today", ""},
		{[]string{"ask", "are you sure"}, Ask, "are you sure", ""},
		{[]string{"context", "lint passed"}, Allow, "", "lint passed"},
		{[]string{"record", filepath.Join(ws, "out.json")}, Allow, "", ""}, // empty stdout
	}
	for _, c := range cases {
		t.Run(c.args[0], func(t *testing.T) {
			res := runHook(context.Background(), v2(t, EventPreToolUse, c.args...), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
			require.Empty(t, res.failed)
			assert.Equal(t, c.decision, res.decision)
			assert.Equal(t, c.reason, res.reason)
			assert.Equal(t, c.context, res.context)
		})
	}
}

func TestV2UpdatedInput(t *testing.T) {
	ws := t.TempDir()
	res := runHook(context.Background(), v2(t, EventPreToolUse, "rewrite", "path", "safe.txt"), ws,
		toolPayload(ws, EventPreToolUse, "write_file", map[string]any{"path": "x.txt", "content": "c"}))
	require.Empty(t, res.failed)
	assert.Equal(t, map[string]any{"path": "safe.txt", "content": "c"}, res.updated)
}

func TestV2MalformedOutputFails(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"garbage", "array", "null", "bad-key", "wrong-case", "bad-decision", "alias", "bad-update"} {
		t.Run(name, func(t *testing.T) {
			res := runHook(context.Background(), v2(t, EventPreToolUse, "canned", name), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
			assert.NotEmpty(t, res.failed)
			assert.Nil(t, res.updated)
		})
	}
}

func TestV2NonZeroExitFails(t *testing.T) {
	ws := t.TempDir()
	res := runHook(context.Background(), v2(t, EventPreToolUse, "exit", "3", "boom"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	assert.Contains(t, res.failed, "exit status 3")
	assert.Contains(t, res.failed, "boom")
}

func TestV2FloodFails(t *testing.T) {
	ws := t.TempDir()
	res := runHook(context.Background(), v2(t, EventPreToolUse, "canned", "flood"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	assert.Contains(t, res.failed, "exceeded")
}

func TestV2FloodForeverCancelsPromptly(t *testing.T) {
	ws := t.TempDir()
	start := time.Now()
	res := runHook(context.Background(), v2(t, EventPreToolUse, "floodforever"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	elapsed := time.Since(start)
	assert.Contains(t, res.failed, "exceeded")
	assert.Less(t, elapsed, 2*time.Second)
}

func TestV2BackgroundChildHoldingStdoutIsKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background job control through sh -c '... &' is Unix-specific")
	}
	ws := t.TempDir()
	marker := filepath.Join(ws, "marker")
	command := "(sleep 3; touch " + marker + ") & echo '{}'"
	def := Definition{Event: EventPreToolUse, Matcher: "*", Command: command, Timeout: DefaultTimeout, Protocol: ProtocolV2}

	start := time.Now()
	res := runHook(context.Background(), def, ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	elapsed := time.Since(start)
	assert.Contains(t, res.failed, "background process kept the hook's output open")
	assert.Less(t, elapsed, 1500*time.Millisecond)

	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err), "background child created marker before hook returned")
	if remaining := 3500*time.Millisecond - time.Since(start); remaining > 0 {
		time.Sleep(remaining)
	}
	_, err = os.Stat(marker)
	assert.True(t, os.IsNotExist(err), "background child survived after hook returned")
}

// Review Focus 3: a hanging hook ends at the caller's deadline on every OS,
// including windows-latest, where the cmd.exe → helper tree is killed.
func TestHookTimeoutFailsClosed(t *testing.T) {
	ws := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := runHook(ctx, v2(t, EventPreToolUse, "sleep"), ws, toolPayload(ws, EventPreToolUse, "bash", nil))
	assert.Contains(t, res.failed, "did not finish")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestPayloadOnStdinAndEnv(t *testing.T) {
	ws := t.TempDir()
	input := map[string]any{"path": "a; b", "command": "go test"}
	stdinFile := filepath.Join(ws, "stdin.json")
	res := runHook(context.Background(), v2(t, EventPreToolUse, "record", stdinFile), ws, toolPayload(ws, EventPreToolUse, "bash", input))
	require.Empty(t, res.failed)
	var got map[string]any
	b, err := os.ReadFile(stdinFile)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "PreToolUse", got["event"])
	assert.Equal(t, "bash", got["tool_name"])
	assert.Equal(t, "s1", got["session_id"])
	assert.Equal(t, map[string]any{"path": "a; b", "command": "go test"}, got["tool_input"])

	envFile := filepath.Join(ws, "env.json")
	res = runHook(context.Background(), v2(t, EventPostToolUse, "env", envFile), ws, toolPayload(ws, EventPostToolUse, "bash", input))
	require.Empty(t, res.failed)
	env := readEnv(t, envFile)
	assert.Equal(t, "PostToolUse", env["CELESTE_HOOK_EVENT"])
	assert.Equal(t, "bash", env["CELESTE_TOOL_NAME"])
	assert.Equal(t, "a; b", env["CELESTE_TOOL_PATH"])
	assert.Equal(t, "go test", env["CELESTE_TOOL_COMMAND"])
	assert.Equal(t, "s1", env["CELESTE_SESSION_ID"])
	assert.Equal(t, ws, env["CELESTE_PROJECT_DIR"])
	assert.Empty(t, env["CELESTE_TOOL_INPUT_TRUNCATED"])
}

func TestInheritedCelesteEnvDoesNotLeak(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("CELESTE_TOOL_COMMAND", "inherited-marker")
	envFile := filepath.Join(ws, "env.json")
	payload := map[string]any{"event": string(EventUserPromptSubmit), "workspace": ws, "project_dir": ws, "session_id": "s1"}
	res := runHook(context.Background(), v2(t, EventUserPromptSubmit, "env", envFile), ws, payload)
	require.Empty(t, res.failed)
	env := readEnv(t, envFile)
	assert.Empty(t, env["CELESTE_TOOL_COMMAND"])
}

// Review Focus 1: a padded command never reaches a hook as a harmless-looking prefix.
func TestPaddedCommandIsOmittedNotTruncated(t *testing.T) {
	ws := t.TempDir()
	padded := "echo safe" + strings.Repeat(" ", 200<<10) + "; rm -rf /"
	envFile := filepath.Join(ws, "env.json")
	res := runHook(context.Background(), v2(t, EventPreToolUse, "env", envFile), ws,
		toolPayload(ws, EventPreToolUse, "bash", map[string]any{"command": padded}))
	require.Empty(t, res.failed)
	env := readEnv(t, envFile)
	assert.Empty(t, env["CELESTE_TOOL_COMMAND"], "omitted, not cut to a plausible prefix")
	assert.Empty(t, env["CELESTE_TOOL_INPUT"])
	assert.Equal(t, "1", env["CELESTE_TOOL_INPUT_TRUNCATED"])
}

// 1.x passed whole inputs; values up to the OS limit must still arrive whole.
func TestMediumInputPassedWhole(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows caps the whole environment block at 32,767 characters; envValueCap is 8 KiB there")
	}
	ws := t.TempDir()
	cmd := "echo " + strings.Repeat("a", 64<<10)
	envFile := filepath.Join(ws, "env.json")
	res := runHook(context.Background(), v2(t, EventPreToolUse, "env", envFile), ws,
		toolPayload(ws, EventPreToolUse, "bash", map[string]any{"command": cmd}))
	require.Empty(t, res.failed)
	assert.Equal(t, cmd, readEnv(t, envFile)["CELESTE_TOOL_COMMAND"])
}

// Review Focus 1: huge inputs and NUL bytes must not break the process start.
func TestLargeToolInputDoesNotBreakHookEnv(t *testing.T) {
	ws := t.TempDir()
	big := strings.Repeat("x", 1<<20)
	input := map[string]any{"path": "a\x00b.txt", "content": big}
	stdinFile := filepath.Join(ws, "stdin.json")
	res := runHook(context.Background(), v2(t, EventPreToolUse, "record", stdinFile), ws, toolPayload(ws, EventPreToolUse, "write_file", input))
	require.Empty(t, res.failed)
	assert.Equal(t, Allow, res.decision)
	b, err := os.ReadFile(stdinFile)
	require.NoError(t, err)
	assert.Contains(t, string(b), big, "the full input still arrives on stdin")
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	assert.Equal(t, "ab", truncate("abc", 2))
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "a", truncate("aé", 2)) // é is 2 bytes; the cut half is dropped
}
