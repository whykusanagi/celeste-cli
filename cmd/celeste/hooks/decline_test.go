package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #411: a decline is stored under the same key and hash as an approval, as
// its own state, and survives a restart (a fresh LoadTrust).
func TestTrustDeclineIsRemembered(t *testing.T) {
	home := testHome(t)
	src := repoSource("/repo/.celeste/hooks.json", "check")
	require.NoError(t, LoadTrust(home).Decline(src))
	assert.Equal(t, Declined, LoadTrust(home).Status(src))
	assert.Equal(t, "declined", Declined.String())

	changed := repoSource(src.Path, "check; other")
	assert.Equal(t, DeclinedChanged, LoadTrust(home).Status(changed), "an edit after a decline asks again")

	require.NoError(t, LoadTrust(home).Approve(src))
	assert.Equal(t, Trusted, LoadTrust(home).Status(src), "an approval replaces the decline")
	require.NoError(t, LoadTrust(home).Decline(src))
	assert.Equal(t, Declined, LoadTrust(home).Status(src), "a decline replaces the approval")
}

// An older celeste reads only "hooks": a decline must never sit there,
// where it would read as an approval.
func TestTrustDeclineNotStoredAsApproval(t *testing.T) {
	home := testHome(t)
	src := repoSource("/repo/.celeste/hooks.json", "check")
	require.NoError(t, LoadTrust(home).Approve(src))
	require.NoError(t, LoadTrust(home).Decline(src))
	data, err := os.ReadFile(TrustPath(home))
	require.NoError(t, err)
	var old struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(data, &old))
	assert.NotContains(t, old.Hooks, src.Path)
}

func TestTrustForget(t *testing.T) {
	home := testHome(t)
	a := repoSource("/a/.celeste/hooks.json", "a")
	b := repoSource("/b/.celeste/hooks.json", "b")
	require.NoError(t, LoadTrust(home).Approve(a))
	require.NoError(t, LoadTrust(home).Decline(b))

	for _, src := range []Source{a, b} {
		forgot, err := LoadTrust(home).Forget(src.Path)
		require.NoError(t, err)
		assert.True(t, forgot)
		assert.Equal(t, Untrusted, LoadTrust(home).Status(src))
	}
	forgot, err := LoadTrust(home).Forget(a.Path)
	require.NoError(t, err)
	assert.False(t, forgot, "nothing left to forget")
}

func TestTrustDeclineRefusedOnCorruptFile(t *testing.T) {
	home := testHome(t)
	writeFile(t, TrustPath(home), "{not json")
	src := repoSource("/r/.celeste/hooks.json", "x")
	assert.Error(t, LoadTrust(home).Decline(src))
	_, err := LoadTrust(home).Forget(src.Path)
	assert.Error(t, err)
	b, err := os.ReadFile(TrustPath(home))
	require.NoError(t, err)
	assert.Equal(t, "{not json", string(b))
}

func TestTrustDeclineGlobalIsNoop(t *testing.T) {
	home := testHome(t)
	src := repoSource(filepath.Join(home, ".celeste", "hooks.json"), "x")
	src.Kind = KindGlobal
	require.NoError(t, LoadTrust(home).Decline(src))
	assert.NoFileExists(t, TrustPath(home))
	assert.Equal(t, Trusted, LoadTrust(home).Status(src))
}

func TestDecide(t *testing.T) {
	home := testHome(t)
	src := repoSource("/repo/.celeste/hooks.json", "check")
	asked := 0
	no := func(Source, TrustStatus) Answer { asked++; return AnswerNo }
	run, why, err := Decide(LoadTrust(home), src, no)
	require.NoError(t, err)
	assert.False(t, run)
	assert.Equal(t, "declined", why)
	assert.Equal(t, 1, asked)

	// Restart: the decline holds and nobody is asked.
	run, why, err = Decide(LoadTrust(home), src, no)
	require.NoError(t, err)
	assert.False(t, run)
	assert.Contains(t, why, "declined")
	assert.Equal(t, 1, asked, "a declined source is not asked about again")

	// Changed since the decline: asked again, with that status.
	var got TrustStatus
	changed := repoSource(src.Path, "check2")
	later := func(_ Source, st TrustStatus) Answer { got = st; return AnswerLater }
	run, why, err = Decide(LoadTrust(home), changed, later)
	require.NoError(t, err)
	assert.False(t, run)
	assert.Equal(t, DeclinedChanged, got)
	assert.Equal(t, "changed since you declined it", why)
	assert.Equal(t, Declined, LoadTrust(home).Status(src), "AnswerLater records nothing")

	yes := func(Source, TrustStatus) Answer { return AnswerYes }
	run, _, err = Decide(LoadTrust(home), changed, yes)
	require.NoError(t, err)
	assert.True(t, run)
	assert.Equal(t, Trusted, LoadTrust(home).Status(changed))

	// No approver (a non-interactive run): never runs, never records.
	other := repoSource("/other/.celeste/hooks.json", "x")
	run, why, err = Decide(LoadTrust(home), other, nil)
	require.NoError(t, err)
	assert.False(t, run)
	assert.Equal(t, "not trusted", why)
	assert.Equal(t, Untrusted, LoadTrust(home).Status(other))
}

func TestPromptApproverAnswers(t *testing.T) {
	src := repoSource("/r/.celeste/hooks.json", "x")
	var out bytes.Buffer
	assert.Equal(t, AnswerYes, PromptApprover(strings.NewReader("y\n"), &out)(src, Untrusted))
	assert.Equal(t, AnswerNo, PromptApprover(strings.NewReader("n\n"), &out)(src, Untrusted))
	assert.Equal(t, AnswerNo, PromptApprover(strings.NewReader("\n"), &out)(src, Untrusted), "Enter is the default no")
	assert.Equal(t, AnswerLater, PromptApprover(strings.NewReader(""), &out)(src, Untrusted), "EOF is no answer: nothing is remembered")
	assert.Contains(t, out.String(), "A no is remembered")

	out.Reset()
	PromptApprover(strings.NewReader("n\n"), &out)(src, DeclinedChanged)
	assert.Contains(t, out.String(), "changed since you declined them")

	mcpSrc := MCPSource(filepath.Join("repo", ".mcp.json"), "srv", `command: "sh"`, "h")
	out.Reset()
	PromptApprover(strings.NewReader("n\n"), &out)(mcpSrc, DeclinedChanged)
	assert.Contains(t, out.String(), "has changed since you declined it")
	assert.Contains(t, out.String(), "celeste mcp trust")
}

// Repo hooks share the prompt and the store with MCP servers, so a no to
// them is remembered too (#411), and an edit asks again.
func TestLoadRemembersDeclinedRepoHooks(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	path := filepath.Join(ws, ".celeste", "hooks.json")
	writeFile(t, path, hooksJSON(t, v2(t, EventPreToolUse, "deny", "x")))
	asked := 0
	no := func(Source, TrustStatus) Answer { asked++; return AnswerNo }
	r, err := Load(Options{Workspace: ws, Home: home, Approve: no, Warn: func(string) {}})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))

	var warnings []string
	r, err = Load(Options{Workspace: ws, Home: home, Approve: no, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	assert.False(t, r.Has(EventPreToolUse))
	assert.Equal(t, 1, asked, "declined hooks are not asked about again")
	assert.Contains(t, strings.Join(warnings, "\n"), "declined")
	assert.Contains(t, strings.Join(warnings, "\n"), "celeste hooks trust")

	writeFile(t, path, hooksJSON(t, v2(t, EventPreToolUse, "deny", "y")))
	_, err = Load(Options{Workspace: ws, Home: home, Approve: no, Warn: func(string) {}})
	require.NoError(t, err)
	assert.Equal(t, 2, asked, "an edit after the decline asks again")
}
