package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

func testRunner(t *testing.T, defs ...Definition) (*Runner, *[]string) {
	t.Helper()
	var warnings []string
	ws := t.TempDir()
	r := &Runner{workspace: ws, sessionID: "s1", warn: func(s string) { warnings = append(warnings, s) }}
	for _, d := range defs {
		r.hooks = append(r.hooks, boundHook{def: d, source: "test", dir: ws})
	}
	return r, &warnings
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestRunnerNilAllows(t *testing.T) {
	var r *Runner
	assert.Equal(t, Allow, r.PreToolUse(context.Background(), "bash", nil).Decision)
	assert.False(t, r.Has(EventStop))
	assert.Nil(t, r.ToolHooks())
}

func TestRunnerDenyShortCircuits(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	r, _ := testRunner(t, v2(t, EventPreToolUse, "record", a), v2(t, EventPreToolUse, "deny", "no"), v2(t, EventPreToolUse, "record", b))
	out := r.PreToolUse(context.Background(), "bash", map[string]any{})
	assert.Equal(t, Deny, out.Decision)
	assert.Equal(t, "no", out.Reason)
	assert.FileExists(t, a)
	assert.NoFileExists(t, b)
}

func TestRunnerAskIsKeptUnlessDenied(t *testing.T) {
	r, _ := testRunner(t, v2(t, EventPreToolUse, "ask", "check"), v2(t, EventPreToolUse, "allow"))
	out := r.PreToolUse(context.Background(), "bash", nil)
	assert.Equal(t, Ask, out.Decision)
	assert.Equal(t, "check", out.Reason)
}

func TestRunnerUpdatedInputChains(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "rec.json")
	r, _ := testRunner(t, v2(t, EventPreToolUse, "rewrite", "path", "b.txt"), v2(t, EventPreToolUse, "record", rec))
	out := r.PreToolUse(context.Background(), "write_file", map[string]any{"path": "a.txt"})
	assert.Equal(t, map[string]any{"path": "b.txt"}, out.UpdatedInput)
	assert.Equal(t, map[string]any{"path": "b.txt"}, readJSON(t, rec)["tool_input"], "later hooks see the rewritten input")
}

func TestRunnerContextJoined(t *testing.T) {
	r, _ := testRunner(t, v2(t, EventPostToolUse, "context", "one"), v2(t, EventPostToolUse, "context", "two"))
	assert.Equal(t, "one\ntwo", r.PostToolUse(context.Background(), "bash", nil, ToolResponse{Content: "ok"}).AdditionalContext)
}

func TestRunnerContextCapped(t *testing.T) {
	r, _ := testRunner(t,
		v2(t, EventPostToolUse, "context", strings.Repeat("a", 3<<10)),
		v2(t, EventPostToolUse, "context", strings.Repeat("b", 3<<10)),
		v2(t, EventPostToolUse, "context", strings.Repeat("c", 3<<10)),
	)
	out := r.PostToolUse(context.Background(), "bash", nil, ToolResponse{Content: "ok"})
	assert.LessOrEqual(t, len(out.AdditionalContext), 8<<10)
}

func TestRunnerObservationalDeniesDoNotShortCircuit(t *testing.T) {
	for _, ev := range []Event{EventPostToolUse, EventSessionStart} {
		t.Run(string(ev), func(t *testing.T) {
			r, _ := testRunner(t, v2(t, ev, "deny", "ignored"), v2(t, ev, "context", "SECOND"))
			out := r.run(context.Background(), ev, "bash", map[string]any{})
			assert.Equal(t, Allow, out.Decision)
			assert.Contains(t, out.AdditionalContext, "SECOND")
		})
	}
}

func TestRunnerStopDenyStillShortCircuits(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "record.json")
	r, _ := testRunner(t, v2(t, EventStop, "deny", "continue"), v2(t, EventStop, "record", rec))
	out := r.Stop(context.Background(), "done")
	assert.Equal(t, Deny, out.Decision)
	assert.Equal(t, "continue", out.Reason)
	assert.NoFileExists(t, rec)
}

func TestRunnerMatcher(t *testing.T) {
	d := v2(t, EventPreToolUse, "deny", "no writes")
	d.Matcher = "write_file"
	r, _ := testRunner(t, d)
	assert.Equal(t, Allow, r.PreToolUse(context.Background(), "read_file", nil).Decision)
	assert.Equal(t, Deny, r.PreToolUse(context.Background(), "write_file", nil).Decision)
}

func TestRunnerFailureFailsClosedOnlyForGatingEvents(t *testing.T) {
	for _, ev := range []Event{EventPreToolUse, EventUserPromptSubmit, EventPreCompact} {
		r, warnings := testRunner(t, v2(t, ev, "canned", "garbage"))
		out := r.run(context.Background(), ev, "bash", map[string]any{})
		assert.Equal(t, Deny, out.Decision, "%s must fail closed", ev)
		assert.True(t, strings.HasPrefix(out.Reason, "hook failed:"))
		assert.Len(t, *warnings, 1)
	}
	for _, ev := range []Event{EventPostToolUse, EventSessionStart, EventPostCompact, EventStop, EventSubagentStop} {
		r, warnings := testRunner(t, v2(t, ev, "canned", "garbage"))
		out := r.run(context.Background(), ev, "bash", map[string]any{})
		assert.Equal(t, Allow, out.Decision, "%s only warns", ev)
		assert.Len(t, *warnings, 1)
	}
}

// A ctx warn sink takes a hook's failure instead of the Runner's own.
func TestRunnerWarnFollowsCtx(t *testing.T) {
	r, warnings := testRunner(t, v2(t, EventPreToolUse, "canned", "garbage"))
	var mine []string
	ctx := WithWarn(context.Background(), func(s string) { mine = append(mine, s) })
	r.run(ctx, EventPreToolUse, "bash", map[string]any{})
	assert.Len(t, mine, 1)
	assert.Empty(t, *warnings)
	r.run(WithWarn(context.Background(), nil), EventPreToolUse, "bash", map[string]any{})
	assert.Len(t, *warnings, 1, "no ctx sink falls back to the Runner's")
}

func TestRunnerEventPayloads(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		ev   Event
		call func(r *Runner)
		want map[string]any
	}{
		{EventSessionStart, func(r *Runner) { r.SessionStart(context.Background(), "resume") }, map[string]any{"source": "resume"}},
		{EventUserPromptSubmit, func(r *Runner) { r.UserPromptSubmit(context.Background(), "hi") }, map[string]any{"prompt": "hi"}},
		{EventPreCompact, func(r *Runner) { r.PreCompact(context.Background(), "manual", "keep todos") }, map[string]any{"trigger": "manual", "custom_instructions": "keep todos"}},
		{EventPostCompact, func(r *Runner) { r.PostCompact(context.Background(), "auto", "S") }, map[string]any{"trigger": "auto", "summary": "S"}},
		{EventStop, func(r *Runner) { r.Stop(context.Background(), "done") }, map[string]any{"last_message": "done"}},
		{EventSubagentStop, func(r *Runner) { r.SubagentStop(context.Background(), "ag1", "sub done") }, map[string]any{"agent_id": "ag1", "last_message": "sub done"}},
		{EventPostToolUse, func(r *Runner) {
			r.PostToolUse(context.Background(), "bash", map[string]any{}, ToolResponse{Content: "out", Error: true})
		},
			map[string]any{"tool_name": "bash", "tool_response": map[string]any{"content": "out", "error": true, "truncated": false}}},
	}
	for _, c := range cases {
		t.Run(string(c.ev), func(t *testing.T) {
			rec := filepath.Join(dir, string(c.ev)+".json")
			r, _ := testRunner(t, v2(t, c.ev, "record", rec))
			c.call(r)
			got := readJSON(t, rec)
			assert.Equal(t, string(c.ev), got["event"])
			assert.Equal(t, "s1", got["session_id"])
			assert.NotEmpty(t, got["workspace"])
			assert.NotEmpty(t, got["project_dir"])
			for k, v := range c.want {
				assert.Equal(t, v, got[k], k)
			}
		})
	}
}

// --- Load and trust gating ---

func hooksJSON(t *testing.T, defs ...Definition) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"hooks": defs})
	require.NoError(t, err)
	return string(b)
}

func TestLoadGlobalHooksRunWithoutApproval(t *testing.T) {
	home := testHome(t)
	writeFile(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventStop, "allow")))
	r, err := Load(Options{Workspace: t.TempDir(), Home: home, Warn: func(string) {}})
	require.NoError(t, err)
	assert.True(t, r.Has(EventStop))
}

func TestLoadSkipsUntrustedRepoHooksNonInteractive(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventPreToolUse, "deny", "x")))
	var warnings []string
	r, err := Load(Options{Workspace: ws, Home: home, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.Contains(t, strings.Join(warnings, "\n"), "celeste hooks trust")
	assert.NoFileExists(t, TrustPath(home), "non-interactive loads never approve")
}

func TestLoadApprovesInteractivelyAndPersists(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventPreToolUse, "deny", "x")))
	var statuses []TrustStatus
	approve := func(src Source, st TrustStatus) Answer { statuses = append(statuses, st); return AnswerYes }
	r, err := Load(Options{Workspace: ws, Home: home, Approve: approve, Warn: func(string) {}})
	require.NoError(t, err)
	assert.True(t, r.Has(EventPreToolUse))
	assert.Equal(t, []TrustStatus{Untrusted}, statuses)

	r, err = Load(Options{Workspace: ws, Home: home, Warn: func(string) {}}) // non-interactive, now trusted
	require.NoError(t, err)
	assert.True(t, r.Has(EventPreToolUse))
}

// Review Focus 4.
func TestLoadChangedRepoHooksReprompt(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	path := filepath.Join(ws, ".celeste", "hooks.json")
	writeFile(t, path, hooksJSON(t, v2(t, EventPreToolUse, "allow")))
	_, err := Load(Options{Workspace: ws, Home: home, Approve: func(Source, TrustStatus) Answer { return AnswerYes }, Warn: func(string) {}})
	require.NoError(t, err)

	writeFile(t, path, hooksJSON(t, v2(t, EventPreToolUse, "deny", "changed")))
	var got []TrustStatus
	_, err = Load(Options{Workspace: ws, Home: home, Approve: func(_ Source, st TrustStatus) Answer { got = append(got, st); return AnswerLater }, Warn: func(string) {}})
	require.NoError(t, err)
	assert.Equal(t, []TrustStatus{Changed}, got)

	var warnings []string
	r, err := Load(Options{Workspace: ws, Home: home, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.Contains(t, strings.Join(warnings, "\n"), "changed since you approved it")
}

// Review Focus 4: a symlink to an approved file does not inherit its trust.
func TestLoadRefusesSymlinkToApprovedFile(t *testing.T) {
	skipSymlinksOnWindows(t)
	home := testHome(t)
	approvedWS := t.TempDir()
	approved := filepath.Join(approvedWS, ".celeste", "hooks.json")
	writeFile(t, approved, hooksJSON(t, v2(t, EventPreToolUse, "allow")))
	_, err := Load(Options{Workspace: approvedWS, Home: home, Approve: func(Source, TrustStatus) Answer { return AnswerYes }, Warn: func(string) {}})
	require.NoError(t, err)

	evil := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(evil, ".celeste"), 0o755))
	require.NoError(t, os.Symlink(approved, filepath.Join(evil, ".celeste", "hooks.json")))
	asked := 0
	r, err := Load(Options{Workspace: evil, Home: home, Approve: func(Source, TrustStatus) Answer { asked++; return AnswerYes }, Warn: func(string) {}})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.Zero(t, asked, "a refused file is never offered for approval")
}

func TestLoadDeclinedApprovalSkips(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventPreToolUse, "allow")))
	r, err := Load(Options{Workspace: ws, Home: home, Approve: func(Source, TrustStatus) Answer { return AnswerLater }, Warn: func(string) {}})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.NoFileExists(t, TrustPath(home))
}

func TestLoadApprovalSaveFailureRunsForSessionOnly(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventStop, "allow")))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".celeste", "trusted.json"), 0o755))
	var warnings []string
	r, err := Load(Options{
		Workspace: ws,
		Home:      home,
		Approve:   func(Source, TrustStatus) Answer { return AnswerYes },
		Warn:      func(s string) { warnings = append(warnings, s) },
	})
	require.NoError(t, err)
	assert.True(t, r.Has(EventStop))
	assert.Contains(t, strings.Join(warnings, "\n"), "approved for this session only")
}

func TestLoadCorruptTrustStoreSkipsRepoHooksWithoutApprover(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t, v2(t, EventStop, "allow")))
	_, err := Load(Options{Workspace: ws, Home: home, Approve: func(Source, TrustStatus) Answer { return AnswerYes }, Warn: func(string) {}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(TrustPath(home), []byte("{not json"), 0o600))

	var warnings []string
	r, err := Load(Options{Workspace: ws, Home: home, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	assert.False(t, r.Has(EventStop))
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, "corrupt")
	assert.Contains(t, joined, "repo hooks stay untrusted")
}

// Spec §3.2: an ancestor directory's grimoire hooks no longer run unasked.
func TestLoadAncestorGrimoireHooksNeedApproval(t *testing.T) {
	home := testHome(t)
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, ".grimoire"), grimoireWithHooks)
	ws := filepath.Join(parent, "sub")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	var warnings []string
	r, err := Load(Options{Workspace: ws, Home: home, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.Contains(t, strings.Join(warnings, "\n"), strconv.Quote(filepath.Join(parent, ".grimoire")))
}

// An approved ancestor hook with a relative path runs in its own project
// root, not the workspace.
func TestLoadAncestorHookRunsInItsRoot(t *testing.T) {
	home := testHome(t)
	parent := t.TempDir()
	ws := filepath.Join(parent, "sub")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	writeFile(t, filepath.Join(parent, ".celeste", "hooks.json"),
		hooksJSON(t, v2(t, EventStop, "record", "out.json"), v2(t, EventStop, "env", "env.json")))
	r, err := Load(Options{Workspace: ws, Home: home, Approve: func(Source, TrustStatus) Answer { return AnswerYes }, Warn: func(string) {}})
	require.NoError(t, err)
	r.Stop(context.Background(), "done")
	assert.FileExists(t, filepath.Join(parent, "out.json"))
	assert.NoFileExists(t, filepath.Join(ws, "out.json"))
	env := readEnv(t, filepath.Join(parent, "env.json"))
	assert.Equal(t, parent, env["CELESTE_PROJECT_DIR"])
	assert.Equal(t, ws, env["CELESTE_WORKSPACE"])
}

func TestHooktestCommandIsQuoted(t *testing.T) {
	assert.True(t, strings.HasPrefix(hooktest.Command(t, "allow"), `"`))
}

func TestDisabledWarning(t *testing.T) {
	got := DisabledWarning(errors.New("boom"))
	if got != "hooks disabled: boom (no hooks run this session, including global guards)" {
		t.Fatalf("got %q", got)
	}
	if DisabledWarning(nil) != "" {
		t.Fatal("nil error must give no warning")
	}
}

// Aikido 806869439: a deny or ask reason can echo model text; it is shown
// in the chat, so it comes back terminal-safe like a failure message.
func TestRunnerReasonIsTerminalSafe(t *testing.T) {
	// No \r: Windows drops it from the hook's command-line argument.
	raw := "blocked: x\x1b]0;t\x07y"
	for _, decision := range []string{"deny", "ask"} {
		r, _ := testRunner(t, v2(t, EventPreToolUse, decision, raw))
		out := r.PreToolUse(context.Background(), "bash", nil)
		assert.Equal(t, SafeText(raw), out.Reason, decision)
		assert.NotContains(t, out.Reason, "\x1b", decision)
	}
}
