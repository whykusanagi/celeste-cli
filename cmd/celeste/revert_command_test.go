package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
)

func changeIn(t *testing.T, session, path, before, after string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpointed(t, checkpoints.NewSnapshotManager(session), path, "call_"+session, after)
}

func revert(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := revertCommand(args, &out, &errb)
	return code, out.String(), errb.String()
}

// The spec's §3.2 finding: a checkpoint taken by a chat or agent session is
// found by the CLI. Without --session, the latest session that touched the
// file wins.
func TestRevertCommandFindsTheLatestSession(t *testing.T) {
	checkpointHome(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	changeIn(t, "chat-old", f, "v0", "v1")
	time.Sleep(50 * time.Millisecond)
	changeIn(t, "agent-run", f, "v1", "v2")

	code, out, _ := revert(t, f)
	if code != 0 || !strings.Contains(out, "before change 1 (session agent-run)") {
		t.Fatalf("revert = %d %q", code, out)
	}
	fileIs(t, f, "v1")

	code, out, _ = revert(t, f, "--session", "chat-old")
	if code != 0 || !strings.Contains(out, "(session chat-old)") {
		t.Fatalf("revert --session = %d %q", code, out)
	}
	fileIs(t, f, "v0")

	code, _, errOut := revert(t, f)
	if code != 1 || !strings.Contains(errOut, "no checkpoint") {
		t.Fatalf("nothing left: %d %q", code, errOut)
	}
}

// --session names a session that never touched the file, or none at all.
func TestRevertCommandUnknownSession(t *testing.T) {
	checkpointHome(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	changeIn(t, "chat-1", f, "v0", "v1")
	changeIn(t, "chat-2", filepath.Join(t.TempDir(), "b.txt"), "x", "y")
	for _, s := range []string{"chat-2", "nope"} {
		code, _, errOut := revert(t, f, "--session="+s)
		if code != 1 || !strings.Contains(errOut, "no checkpoint") {
			t.Fatalf("--session %s: %d %q", s, code, errOut)
		}
	}
	fileIs(t, f, "v1")
}

// Review Focus 3: a relative path from the file's directory, and the
// flag before the file.
func TestRevertCommandRelativePath(t *testing.T) {
	checkpointHome(t)
	dir := t.TempDir()
	changeIn(t, "chat-1", filepath.Join(dir, "a.txt"), "before", "after")
	t.Chdir(dir)
	code, out, errOut := revert(t, "--session=chat-1", "a.txt")
	if code != 0 {
		t.Fatalf("revert = %d %q %q", code, out, errOut)
	}
	fileIs(t, filepath.Join(dir, "a.txt"), "before")
}

// Review Focus 3: the file through a symlinked directory.
func TestRevertCommandThroughASymlink(t *testing.T) {
	checkpointHome(t)
	dir := t.TempDir()
	changeIn(t, "chat-1", filepath.Join(dir, "a.txt"), "before", "after")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code, out, errOut := revert(t, filepath.Join(link, "a.txt")); code != 0 {
		t.Fatalf("revert = %d %q %q", code, out, errOut)
	}
	fileIs(t, filepath.Join(dir, "a.txt"), "before")
}

func TestRevertCommandUndoesACreation(t *testing.T) {
	checkpointHome(t)
	f := filepath.Join(t.TempDir(), "new.txt")
	checkpointed(t, checkpoints.NewSnapshotManager("chat-1"), f, "call_1", "x")
	code, out, _ := revert(t, f)
	if code != 0 || !strings.Contains(out, "Removed") {
		t.Fatalf("revert = %d %q", code, out)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("the created file is still there")
	}
}

func TestRevertCommandArguments(t *testing.T) {
	checkpointHome(t)
	for _, args := range [][]string{{}, {"--session"}, {"a.txt", "b.txt"}, {"a.txt", "--session="}, {"--bogus", "a.txt"}} {
		if code, _, errOut := revert(t, args...); code != 2 || errOut == "" {
			t.Fatalf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
	if code, out, _ := revert(t, "--help"); code != 0 || !strings.Contains(out, "Usage: celeste revert") {
		t.Fatalf("--help: %d %q", code, out)
	}
}
