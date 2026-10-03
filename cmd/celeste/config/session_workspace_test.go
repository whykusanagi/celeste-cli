package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sessionHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// 2.0 W4 ruling 1: a session keeps its workspace; an older file without
// the key loads unchanged, with no workspace.
func TestSessionWorkspaceRoundTrip(t *testing.T) {
	sessionHome(t)
	mgr := NewSessionManager()
	ws := t.TempDir()
	s := mgr.NewSession()
	s.SetWorkspace(ws)
	if err := mgr.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.Load(s.ID)
	if err != nil || got.GetWorkspace() != ws {
		t.Fatalf("workspace = %q (%v), want %q", got.GetWorkspace(), err, ws)
	}
	if sum := got.Summarize(); sum.Workspace != ws {
		t.Fatalf("summary workspace = %q", sum.Workspace)
	}

	old := `{"id":"123","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","messages":[]}`
	if err := os.WriteFile(filepath.Join(mgr.sessionsDir, "123.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = mgr.Load("123")
	if err != nil || got.Workspace != "" {
		t.Fatalf("old session: workspace %q, %v", got.Workspace, err)
	}
}

func TestSameProject(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{repo, repo, true},
		{sub, repo, true},
		{repo + string(filepath.Separator), sub, true},
		{other, repo, false},
		{other, other, true},
		{"", repo, false},
		{repo, "", false},
	} {
		if got := SameProject(c.a, c.b); got != c.want {
			t.Errorf("SameProject(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// Ruling 2: this project's sessions (same git root) first, newest first;
// then the rest, newest first.
func TestSortForWorkspace(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "tools")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	now := time.Now()
	at := func(id, ws string, ago int) Session {
		return Session{ID: id, Workspace: ws, UpdatedAt: now.Add(-time.Duration(ago) * time.Hour)}
	}
	sessions := []Session{
		at("old-here", repo, 5),
		at("elsewhere", elsewhere, 1),
		at("none", "", 0),
		at("sub", sub, 2),
		at("old-elsewhere", elsewhere, 9),
	}
	mine, others := SortForWorkspace(sessions, repo)
	ids := func(ss []Session) (out []string) {
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	if got := ids(mine); len(got) != 2 || got[0] != "sub" || got[1] != "old-here" {
		t.Fatalf("mine = %v", got)
	}
	if got := ids(others); len(got) != 3 || got[0] != "none" || got[1] != "elsewhere" || got[2] != "old-elsewhere" {
		t.Fatalf("others = %v", got)
	}
}
