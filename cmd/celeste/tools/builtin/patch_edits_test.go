package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/checkpoints"
)

// patchEnv is a workspace with a tracker that has read every file it wrote.
func patchEnv(t *testing.T, files map[string]string) (string, *PatchFileTool, *checkpoints.FileTracker) {
	t.Helper()
	ws := t.TempDir()
	ft := checkpoints.NewFileTracker()
	for name, body := range files {
		p := filepath.Join(ws, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = ft.RecordRead(p)
	}
	return ws, NewPatchFileTool(ws, WithPatchFileTracker(ft)), ft
}

func TestMultiEditAppliesInOrder(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"a.go": "one\ntwo\nthree\n"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "one", "new_string": "ONE"},
		map[string]any{"old_string": "ONE\ntwo", "new_string": "ONE\nTWO"},
	}}, nil)
	if res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.go")); string(b) != "ONE\nTWO\nthree\n" {
		t.Fatalf("file = %q", b)
	}
	if res.Metadata["edits"] != 2 {
		t.Fatalf("result = %+v", res.Metadata)
	}
	if rs, ok := res.Metadata["results"].([]map[string]any); !ok || len(rs) != 2 || rs[1]["replacements"] != 1 {
		t.Fatalf("results = %#v", res.Metadata["results"])
	}
}

// Review Focus 3.
func TestMultiEditIsAllOrNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws, tool, _ := patchEnv(t, map[string]string{"a.go": "one\ntwo\nthree\n"})
	snaps := checkpoints.NewSnapshotManager("multi-edit-test")
	tool.snapMgr = snaps
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "one", "new_string": "1"},
		map[string]any{"old_string": "two", "new_string": "2"},
		map[string]any{"old_string": "missing", "new_string": "x"},
	}}, nil)
	if !res.Error || !strings.Contains(res.Content, "edit 3") {
		t.Fatalf("result = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.go")); string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("file changed: %q", b)
	}
	if entries := snaps.Entries(); len(entries) != 0 {
		t.Fatalf("a failed edit took a checkpoint: %+v", entries)
	}
}

func TestPatchRejectsBothShapes(t *testing.T) {
	_, tool, _ := patchEnv(t, map[string]string{"a.go": "x"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "a.go", "old_string": "x", "new_string": "y",
		"edits": []any{map[string]any{"old_string": "x", "new_string": "z"}}}, nil)
	if !res.Error || !strings.Contains(res.Content, "either") {
		t.Fatalf("result = %+v", res)
	}
}

func TestMultiEditOversizedLiteralNamesTheEdit(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"a.go": "one\ntwo\n"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "one", "new_string": "1"},
		map[string]any{"old_string": "two", "new_string": strings.Repeat("x", maxPatchLiteralBytes+1)},
	}}, nil)
	if !res.Error || !strings.Contains(res.Content, "edit 2") || !strings.Contains(res.Content, "splice_file") {
		t.Fatalf("result = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.go")); string(b) != "one\ntwo\n" {
		t.Fatalf("file changed: %q", b)
	}
}

func TestPatchEditsShapeErrors(t *testing.T) {
	_, tool, _ := patchEnv(t, map[string]string{"a.go": "x"})
	for name, input := range map[string]map[string]any{
		"neither":       {"path": "a.go"},
		"empty edits":   {"path": "a.go", "edits": []any{}},
		"not object":    {"path": "a.go", "edits": []any{"x"}},
		"no old":        {"path": "a.go", "edits": []any{map[string]any{"new_string": "y"}}},
		"no new":        {"path": "a.go", "edits": []any{map[string]any{"old_string": "x"}}},
		"single no new": {"path": "a.go", "old_string": "x"},
	} {
		if res, _ := tool.Execute(context.Background(), input, nil); !res.Error {
			t.Errorf("%s: accepted: %+v", name, res)
		}
	}
}
