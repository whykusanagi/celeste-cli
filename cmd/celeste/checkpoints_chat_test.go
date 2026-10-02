package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
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

// rewindChat runs a chat on a fake provider where turn 1 creates a.go and
// b.go and patches c.go (calls w1, w2, p1, after reading c.go: r0) and
// turn 2 only reads (r1).
func rewindChat(t *testing.T) (tea.Model, *chatDeps, string) {
	t.Helper()
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r0", Name: "read_file", Args: `{"path":"c.go"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "w1", Name: "write_file", Args: `{"path":"a.go","content":"package a\n"}`},
			{ID: "w2", Name: "write_file", Args: `{"path":"b.go","content":"package b\n"}`},
			{ID: "p1", Name: "patch_file", Args: `{"path":"c.go","old_string":"old","new_string":"new"}`},
		}},
		fakeprovider.Turn{Text: "Wrote them."},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"a.go"}`}}},
		fakeprovider.Turn{Text: "Read it."},
	)
	m, deps, ws := chatApp(t, srv)
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	if err := os.WriteFile(filepath.Join(ws, "c.go"), []byte("package c // old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "untouched.go"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write the files"}},
		func(m tea.Model) bool { return lastAssistant(m) == "Wrote them." && turnIdle(m) }, 30*time.Second)
	fileIs(t, filepath.Join(ws, "c.go"), "package c // new\n")
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.go"}},
		func(m tea.Model) bool { return lastAssistant(m) == "Read it." && turnIdle(m) }, 30*time.Second)
	return m, deps, ws
}

func hasChatLine(m tea.Model, s string) bool {
	for _, x := range chatMessages(m) {
		if x.Role == "system" && strings.Contains(x.Content, s) {
			return true
		}
	}
	return false
}

// Review Focus 1, through the real adapter and store.
func TestRewindRestoresFilesFromTheCheckpointIndex(t *testing.T) {
	m, _, ws := rewindChat(t)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/rewind 2"}},
		func(m tea.Model) bool { return hasChatLine(m, "Rewound 2 prompt(s)") }, 30*time.Second)
	for _, f := range []string{"a.go", "b.go"} {
		if _, err := os.Stat(filepath.Join(ws, f)); !os.IsNotExist(err) {
			t.Fatalf("%s was created by the rewound turn and must be gone", f)
		}
	}
	fileIs(t, filepath.Join(ws, "c.go"), "package c // old\n")
	fileIs(t, filepath.Join(ws, "untouched.go"), "keep")
	// Calls in one response may run in any order: the names, not their order.
	var line string
	for _, x := range chatMessages(m) {
		if strings.Contains(x.Content, "restored 3 file change(s): ") {
			line = x.Content
		}
	}
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(line, f) {
			t.Fatalf("rewind line %q does not name %s; chat = %+v", line, f, chatMessages(m))
		}
	}
	for _, x := range chatMessages(m) {
		if x.Role != "system" {
			t.Fatalf("the chat must end before turn 1's prompt; still has %s %q", x.Role, x.Content)
		}
	}
	if got := m.(tui.AppModel).DebugInput(); got != "write the files" {
		t.Fatalf("input = %q", got)
	}
	// /undo and /diff still work: nothing is left to undo.
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/diff"}},
		func(m tea.Model) bool { return hasChatLine(m, "No files changed in this session.") }, 30*time.Second)
	_ = m
}

func TestRewindWithoutWritesChangesNoFiles(t *testing.T) {
	m, deps, ws := rewindChat(t)
	before := len(deps.env.Snapshots.Entries())
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/rewind 1"}},
		func(m tea.Model) bool { return hasChatLine(m, "Rewound 1 prompt(s)") }, 30*time.Second)
	if !hasChatLine(m, "no files were changed by those turns") {
		t.Fatalf("chat = %+v", chatMessages(m))
	}
	if n := len(deps.env.Snapshots.Entries()); n != before {
		t.Fatalf("entries %d -> %d", before, n)
	}
	fileIs(t, filepath.Join(ws, "a.go"), "package a\n")
	fileIs(t, filepath.Join(ws, "c.go"), "package c // new\n")
	if got := m.(tui.AppModel).DebugInput(); got != "read a.go" {
		t.Fatalf("input = %q", got)
	}
}

// A file changed outside celeste after the rewound turns: the first
// /rewind changes nothing and says so; the same /rewind again overwrites.
func TestRewindAsksBeforeOverwritingAnOutsideChange(t *testing.T) {
	checkpointHome(t)
	ws := t.TempDir()
	f := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := checkpoints.NewSnapshotManager("chat-rw")
	checkpointed(t, sm, f, "call_1", "v1")
	if err := os.WriteFile(f, []byte("edited by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &TUIClientAdapter{snapshots: sm, workspace: ws}
	if _, err := a.RewindTo([]string{"call_1"}); err == nil || !strings.Contains(err.Error(), "a.txt changed") {
		t.Fatalf("first /rewind err = %v", err)
	}
	fileIs(t, f, "edited by hand")
	paths, err := a.RewindTo([]string{"call_1"})
	if err != nil || len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("second /rewind = %v, %v", paths, err)
	}
	fileIs(t, f, "v0")
	if paths, err := a.RewindTo([]string{"call_1"}); err != nil || len(paths) != 0 {
		t.Fatalf("nothing left: %v, %v", paths, err)
	}
}
