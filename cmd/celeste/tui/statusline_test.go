package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDirtyCount(t *testing.T) {
	assert.Equal(t, 0, parseDirtyCount(""))
	assert.Equal(t, 0, parseDirtyCount("\n  \n"))
	assert.Equal(t, 2, parseDirtyCount(" M internal/a.go\n?? b.txt"))
	assert.Equal(t, 3, parseDirtyCount("A  x\n M y\nD  z\n"))
}

func TestParseAheadBehind(t *testing.T) {
	// `git rev-list --left-right --count @{u}...HEAD` prints "<behind>\t<ahead>".
	ahead, behind := parseAheadBehind("1\t2\n")
	assert.Equal(t, 2, ahead)
	assert.Equal(t, 1, behind)

	ahead, behind = parseAheadBehind("garbage")
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 0, behind)
}

func TestStatusLineView_FullWidth(t *testing.T) {
	m := NewStatusLineModel().
		SetWidth(160).
		SetGit("feat/mcp-connectivity", 3, 2, 0).
		SetProject("celeste-cli").
		SetModel("fugu").
		SetEffort("medium").
		SetPermMode("trust").
		SetSession("night-session")

	out := m.View()
	assert.Contains(t, out, "feat/mcp-connectivity")
	assert.Contains(t, out, "✎3")
	assert.Contains(t, out, "↑2")
	assert.Contains(t, out, "celeste-cli")
	assert.Contains(t, out, "fugu")
	assert.Contains(t, out, "medium")
	assert.Contains(t, out, "trust")
	assert.Contains(t, out, "night-session")
}

func TestStatusLineView_Narrow_DropsSession(t *testing.T) {
	m := NewStatusLineModel().
		SetWidth(40).
		SetGit("main", 0, 0, 0).
		SetModel("fugu").
		SetSession("night-session")

	out := m.View()
	assert.Contains(t, out, "main")
	assert.Contains(t, out, "fugu")
	assert.False(t, strings.Contains(out, "night-session"), "narrow bar must drop the session segment")
}

func TestStatusLineView_ZeroWidthEmpty(t *testing.T) {
	assert.Equal(t, "", NewStatusLineModel().View())
}

func TestGitFetchCmd_NonRepo(t *testing.T) {
	dir := t.TempDir() // fresh temp dir is not a git repo
	msg := gitFetchCmd(dir)()
	gs, ok := msg.(GitStatusMsg)
	if !ok {
		t.Fatalf("expected GitStatusMsg, got %T", msg)
	}
	if gs.Repo {
		t.Fatalf("expected Repo=false outside a git repo, got true")
	}
}

func TestSyncStatusLine_PopulatesNonGitSegments(t *testing.T) {
	m := AppModel{
		model:      "fugu",
		effort:     "high",
		workDir:    "/home/dev/celeste-cli",
		statusLine: NewStatusLineModel().SetWidth(160),
	}
	m = m.syncStatusLine()
	out := m.statusLine.View()
	assert.Contains(t, out, "fugu")
	assert.Contains(t, out, "high")
	assert.Contains(t, out, "celeste-cli") // filepath.Base(workDir)
}

func TestHintsFor(t *testing.T) {
	// Chat (default) view.
	chat := hintsFor("chat", false)
	assert.Contains(t, chat, "send")
	assert.Contains(t, chat, "/ commands")

	// A panel view.
	skills := hintsFor("skills", false)
	assert.Contains(t, skills, "select")
	assert.Contains(t, skills, "esc close")
	assert.NotContains(t, skills, "send")

	// MCP panel active overrides the mode hints.
	mcp := hintsFor("chat", true)
	assert.Contains(t, mcp, "connect")
	assert.Contains(t, mcp, "disconnect")
}
