package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook executor uses sh -c which is not available on Windows")
	}
}

func TestExecutor_PreToolUse_Approve(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "true", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, 0, result.ExitCode)
}

func TestExecutor_PreToolUse_Block(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "echo blocked && exit 1", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "block", result.Decision)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "blocked", result.Output)
}

func TestExecutor_ToolFiltering(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "write_file", Command: "exit 1", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())

	// Should not match "bash"
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)

	// Should match "write_file"
	result, err = exec.RunPreToolUse("write_file", nil)
	require.NoError(t, err)
	assert.Equal(t, "block", result.Decision)
}

func TestExecutor_PostToolUse(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PostToolUse", Tool: "*", Command: "echo done", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPostToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, "done", result.Output)
}

func TestExecutor_TemplateVars(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "echo {{tool}} {{path}}", Timeout: 5},
	}
	workspace := t.TempDir()
	exec := NewExecutor(hooks, workspace)
	result, err := exec.RunPreToolUse("write_file", map[string]any{"path": "/tmp/test.txt"})
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, "write_file /tmp/test.txt", result.Output)
}

func TestExpandTemplateVars(t *testing.T) {
	result := expandTemplateVars(
		"cd {{workspace}} && {{tool}} {{path}} {{command}}",
		"/home/user/project",
		"bash",
		map[string]any{"path": "src/main.go", "command": "go build"},
	)
	assert.Equal(t, "cd '/home/user/project' && 'bash' 'src/main.go' 'go build'", result)
}

func TestExpandTemplateVars_MissingInput(t *testing.T) {
	result := expandTemplateVars("echo {{path}} {{command}}", "/ws", "test", nil)
	assert.Equal(t, "echo '' ''", result)
}

func TestExecutor_NoMatchingHooks(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PostToolUse", Tool: "bash", Command: "echo post", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	// PreToolUse should not match PostToolUse hooks
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
}

// TestExecutor_TemplateInjection covers #168: {{path}} and {{command}} come
// from model-chosen tool arguments and used to reach `sh -c` unquoted.
func TestExecutor_TemplateInjection(t *testing.T) {
	skipOnWindows(t)
	// PWNED is replaced per subtest with a marker file path; each case would
	// create it if its payload reached the shell as code.
	cases := map[string]struct {
		command string
		input   map[string]any
	}{
		"path semicolon":        {"echo {{path}}", map[string]any{"path": "a; touch PWNED"}},
		"path command subst":    {"echo {{path}}", map[string]any{"path": "$(touch PWNED)"}},
		"path backticks":        {"echo {{path}}", map[string]any{"path": "`touch PWNED`"}},
		"command in dquotes":    {`echo "{{command}}"`, map[string]any{"command": `"; touch PWNED; echo "`}},
		"single quote breakout": {"echo {{path}}", map[string]any{"path": "x'; touch PWNED; echo '"}},
		// A value containing another placeholder must not pull a second
		// substitution inside its quotes.
		"nested placeholder": {"echo {{path}} {{command}}", map[string]any{
			"path":    "a {{command}} b",
			"command": "; touch PWNED;",
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			pwned := filepath.Join(workspace, "pwned")
			input := map[string]any{}
			for k, v := range tc.input {
				input[k] = strings.ReplaceAll(v.(string), "PWNED", pwned)
			}
			hooks := []Hook{{Event: "PreToolUse", Tool: "*", Command: tc.command, Timeout: 5}}
			_, err := NewExecutor(hooks, workspace).RunPreToolUse("read_file", input)
			require.NoError(t, err)
			_, statErr := os.Stat(pwned)
			assert.True(t, os.IsNotExist(statErr), "hook command injection created %s", pwned)
		})
	}
}

func TestExecutor_TemplateValuePreserved(t *testing.T) {
	skipOnWindows(t)
	cases := map[string]string{
		"bare":          "echo {{path}}",
		"double quoted": `echo "{{path}}"`,
		"single quoted": "echo '{{path}}'",
	}
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			hooks := []Hook{{Event: "PreToolUse", Tool: "*", Command: command, Timeout: 5}}
			result, err := NewExecutor(hooks, t.TempDir()).RunPreToolUse("read_file",
				map[string]any{"path": "dir with space/it's $HOME.go"})
			require.NoError(t, err)
			assert.Equal(t, "dir with space/it's $HOME.go", result.Output)
		})
	}
}

func TestExecutor_PayloadOnStdinAndEnv(t *testing.T) {
	skipOnWindows(t)
	workspace := t.TempDir()
	input := map[string]any{"path": "a; b", "command": "go test"}

	stdinHook := []Hook{{Event: "PreToolUse", Tool: "*", Command: "cat", Timeout: 5}}
	result, err := NewExecutor(stdinHook, workspace).RunPreToolUse("bash", input)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Output), &payload))
	assert.Equal(t, "PreToolUse", payload["event"])
	assert.Equal(t, "bash", payload["tool_name"])
	assert.Equal(t, workspace, payload["workspace"])
	assert.Equal(t, map[string]any{"path": "a; b", "command": "go test"}, payload["tool_input"])

	envHook := []Hook{{Event: "PostToolUse", Tool: "*",
		Command: `printf '%s|%s|%s|%s' "$CELESTE_HOOK_EVENT" "$CELESTE_TOOL_NAME" "$CELESTE_TOOL_PATH" "$CELESTE_TOOL_COMMAND"`,
		Timeout: 5}}
	result, err = NewExecutor(envHook, workspace).RunPostToolUse("bash", input)
	require.NoError(t, err)
	assert.Equal(t, "PostToolUse|bash|a; b|go test", result.Output)
}
