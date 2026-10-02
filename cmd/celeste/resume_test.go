package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// celeste resume finds TUI sessions by ID or by name (#188). It used to read a
// store nothing writes.
func TestFindSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	mgr := config.NewSessionManager()
	s := mgr.NewSession()
	s.Name = "Refactor Plans"
	s.Messages = append(s.Messages, config.SessionMessage{Role: "user", Content: "hi"})
	if err := mgr.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	for _, key := range []string{s.ID, "refactor plans"} {
		got, err := findSession(mgr, key)
		if err != nil {
			t.Fatalf("findSession(%q): %v", key, err)
		}
		if got.ID != s.ID {
			t.Errorf("findSession(%q) = %s, want %s", key, got.ID, s.ID)
		}
	}
	if _, err := findSession(mgr, "nope"); err == nil {
		t.Error("findSession should fail for an unknown session")
	}
}

// 2.0 W4 ruling 2: `celeste resume` lists this project's sessions first,
// each marked; the rest follow unmarked.
func TestResumeListsThisProjectFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := config.NewSessionManager()
	save := func(name, ws string) {
		s := mgr.NewSession()
		s.Name = name
		s.Workspace = ws
		if err := mgr.Save(s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // distinct UpdatedAt
	}
	save("here old", repo)
	save("elsewhere", t.TempDir())
	save("no workspace", "")
	save("here new", repo)

	var buf bytes.Buffer
	if err := printSessionList(&buf, mgr, filepath.Join(repo, "sub")); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(l, "  ") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 4 {
		t.Fatalf("lines = %q", lines)
	}
	for i, want := range []string{"here new", "here old"} {
		if !strings.Contains(lines[i], want) || !strings.HasSuffix(lines[i], " (this project)") {
			t.Fatalf("line %d = %q, want %q marked as this project", i, lines[i], want)
		}
	}
	for i, want := range []string{"no workspace", "elsewhere"} {
		if l := lines[i+2]; !strings.Contains(l, want) || strings.Contains(l, "this project") {
			t.Fatalf("line %d = %q, want %q unmarked", i+2, l, want)
		}
	}
}
