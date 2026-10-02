package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// celeste init [--agents] (ruling 7): --agents also writes AGENTS.md, even
// when .grimoire already exists; nothing is overwritten.
func TestInitProjectAgentsFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	var out bytes.Buffer
	if err := initProject(ws, nil, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("celeste init without --agents wrote AGENTS.md")
	}
	out.Reset()
	if err := initProject(ws, []string{"--agents"}, &out); err != nil {
		t.Fatalf("an existing .grimoire must not stop --agents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Fatalf("--agents did not write AGENTS.md: %v", err)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Fatalf("output should note the existing .grimoire:\n%s", out.String())
	}
	err := initProject(ws, []string{"--agents"}, &out)
	if err == nil {
		t.Fatal("nothing left to write must be an error")
	}
	if msg := initErrorText(err); msg != "" {
		t.Fatalf("the skip lines already say why; stderr should not repeat it: %q", msg)
	}
	if msg := initErrorText(errors.New("boom")); msg != "Error: boom" {
		t.Fatalf("another failure is printed: %q", msg)
	}
}

// celeste grimoire shows the context files under the grimoire, and is not
// "empty" when only an AGENTS.md exists.
func TestShowGrimoireIncludesContextFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("use tabs"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := showGrimoire(ws, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "# Project instructions") || !strings.Contains(out.String(), "use tabs") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// celeste init -h prints the usage and is not an error.
func TestInitProjectHelp(t *testing.T) {
	ws := t.TempDir()
	var out bytes.Buffer
	if err := initProject(ws, []string{"-h"}, &out); err != nil {
		t.Fatalf("-h must not be an error: %v", err)
	}
	if !strings.Contains(out.String(), "-agents") {
		t.Fatalf("-h should print the usage:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(ws, ".grimoire")); !os.IsNotExist(err) {
		t.Fatal("-h must not write anything")
	}
}
