package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func checkpointHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// checkpointed records path's current state in session, then writes after.
func checkpointed(t *testing.T, sm *checkpoints.SnapshotManager, path, callID, after string) {
	t.Helper()
	c, err := sm.Checkpoint(path, callID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(); err != nil {
		t.Fatal(err)
	}
}

func fileIs(t *testing.T, path, want string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Fatalf("%s = %q (%v), want %q", filepath.Base(path), b, err, want)
	}
}

// Review Focus 4: a resumed session undoes what the earlier process changed.
func TestChatUndoAfterResume(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpointed(t, checkpoints.NewSnapshotManager("chat-1"), f, "call_1", "after")

	a := &TUIClientAdapter{snapshots: checkpoints.NewSnapshotManager("chat-1"), workspace: ws}
	diff, err := a.SessionChanges()
	if err != nil || diff != "Files changed this session:\n  a.txt  +1 -1" {
		t.Fatalf("/diff = %q, %v", diff, err)
	}
	msg, err := a.UndoLastChange()
	if err != nil || msg != "Restored a.txt to its state before change 1." {
		t.Fatalf("/undo = %q, %v", msg, err)
	}
	fileIs(t, f, "before")
	if _, err := a.UndoLastChange(); err == nil || !strings.Contains(err.Error(), "no file changes to undo") {
		t.Fatalf("second /undo err = %v", err)
	}
}

// Several edits to one file: /diff compares with the state before the
// first, and /undo walks back one edit at a time.
func TestChatUndoWalksBackEditsToOneFile(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := checkpoints.NewSnapshotManager("chat-3")
	checkpointed(t, sm, f, "call_1", "v1")
	checkpointed(t, sm, f, "call_2", "v2\nmore")
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	if diff, _ := a.SessionChanges(); diff != "Files changed this session:\n  a.txt  +2 -1" {
		t.Fatalf("/diff = %q", diff)
	}
	if msg, err := a.UndoLastChange(); err != nil || msg != "Restored a.txt to its state before change 2." {
		t.Fatalf("/undo = %q, %v", msg, err)
	}
	fileIs(t, f, "v1")
	if msg, err := a.UndoLastChange(); err != nil || msg != "Restored a.txt to its state before change 1." {
		t.Fatalf("second /undo = %q, %v", msg, err)
	}
	fileIs(t, f, "v0")
	if diff, _ := a.SessionChanges(); diff != "No files changed in this session." {
		t.Fatalf("/diff after undoing everything = %q", diff)
	}
}

// A file changed after celeste's last change to it (an editor, a
// formatter, bash — even a second later): the first /undo says so and
// leaves it alone; a second /undo, with the file as it was warned about,
// overwrites it.
func TestChatUndoAsksBeforeOverwritingAnOutsideChange(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := checkpoints.NewSnapshotManager("chat-4")
	checkpointed(t, sm, f, "call_1", "v1")
	if err := os.WriteFile(f, []byte("edited by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	_, err := a.UndoLastChange()
	if err == nil || !strings.Contains(err.Error(), "a.txt changed after celeste's last change to it") || !strings.Contains(err.Error(), "/undo again to overwrite it") {
		t.Fatalf("first /undo err = %v", err)
	}
	fileIs(t, f, "edited by hand")
	if msg, err := a.UndoLastChange(); err != nil || msg != "Restored a.txt to its state before change 1." {
		t.Fatalf("confirmed /undo = %q, %v", msg, err)
	}
	fileIs(t, f, "v0")
}

// Review M1: the confirmation is for the file as it was warned about. An
// edit after the warning warns again.
func TestChatUndoWarnsAgainWhenTheFileChangesAfterTheWarning(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := checkpoints.NewSnapshotManager("chat-5")
	checkpointed(t, sm, f, "call_1", "v1")
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	for _, edit := range []string{"hand edit 1", "hand edit 2"} {
		if err := os.WriteFile(f, []byte(edit), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := a.UndoLastChange(); err == nil {
			t.Fatalf("/undo after %q overwrote it", edit)
		}
		fileIs(t, f, edit)
	}
}

// Review M2: undoing the creation of a file changed since says the file
// will be deleted.
func TestChatUndoOfAChangedCreationSaysItDeletes(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	sm := checkpoints.NewSnapshotManager("chat-6")
	f := filepath.Join(ws, "new.txt")
	checkpointed(t, sm, f, "call_1", "x")
	if err := os.WriteFile(f, []byte("x and more by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	if _, err := a.UndoLastChange(); err == nil || !strings.Contains(err.Error(), "new.txt changed after celeste created it") || !strings.Contains(err.Error(), "/undo again to delete it") {
		t.Fatalf("/undo err = %v", err)
	}
	if msg, err := a.UndoLastChange(); err != nil || msg != "Undid the creation of new.txt (deleted it)." {
		t.Fatalf("confirmed /undo = %q, %v", msg, err)
	}
}

// Review I1: while a write (a background subagent's) is between its
// checkpoint and its commit, /undo refuses and changes nothing.
func TestChatUndoWaitsForAWriteInProgress(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := checkpoints.NewSnapshotManager("chat-7")
	checkpointed(t, sm, f, "call_1", "v1")
	c, err := sm.Checkpoint(filepath.Join(ws, "b.txt"), "call_bg")
	if err != nil {
		t.Fatal(err)
	}
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	if _, err := a.UndoLastChange(); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("/undo during a write: %v", err)
	}
	fileIs(t, f, "v1")
	if err := c.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UndoLastChange(); err != nil {
		t.Fatal(err)
	}
	fileIs(t, f, "v0")
}

func TestChatUndoOfACreation(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	sm := checkpoints.NewSnapshotManager("chat-2")
	f := filepath.Join(ws, "new.txt")
	checkpointed(t, sm, f, "call_1", "x")
	msg, err := (&TUIClientAdapter{snapshots: sm, workspace: ws}).UndoLastChange()
	if err != nil || msg != "Undid the creation of new.txt (deleted it)." {
		t.Fatalf("/undo = %q, %v", msg, err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("the created file is still there")
	}
}

func TestChatAdapterWithoutCheckpoints(t *testing.T) {
	if _, err := (&TUIClientAdapter{}).UndoLastChange(); err == nil {
		t.Fatal("want an error with no store")
	}
	if _, err := (&TUIClientAdapter{}).SessionChanges(); err == nil {
		t.Fatal("want an error with no store")
	}
}

// The chat's adapter uses the chat Env's store: a write through the chat's
// registry shows in /diff and /undo restores it.
func TestChatWiresCheckpointsToTheEnv(t *testing.T) {
	_, deps, _ := chatApp(t, fakeprovider.NewOpenAI(t))
	if deps.adapter.snapshots != deps.env.Snapshots || deps.adapter.workspace != deps.env.Workspace {
		t.Fatal("the adapter must use the chat Env's checkpoint store and workspace")
	}
	if !strings.HasPrefix(deps.env.Snapshots.Dir(), checkpoints.Root()) {
		t.Fatalf("store %s is not under %s", deps.env.Snapshots.Dir(), checkpoints.Root())
	}
	tool, _ := deps.registry.Get("write_file")
	res, err := tool.Execute(tools.WithCallID(context.Background(), "call_1"), map[string]any{"path": "wired.txt", "content": "x"}, nil)
	if err != nil || res.Error {
		t.Fatalf("write_file: %v %s", err, res.Content)
	}
	diff, _ := deps.adapter.SessionChanges()
	if !strings.Contains(diff, "wired.txt  +1 -0 (new)") {
		t.Fatalf("/diff = %q", diff)
	}
	if _, err := deps.adapter.UndoLastChange(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(deps.env.Workspace, "wired.txt")); !os.IsNotExist(err) {
		t.Fatal("/undo did not remove the created file")
	}
}
