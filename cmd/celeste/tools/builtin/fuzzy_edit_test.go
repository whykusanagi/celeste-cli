package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goFile = "package x\n\nfunc f() {\n\tif ok {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"

// Review Focus 1.
func TestFuzzyMatchUniqueReindents(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"x.go": goFile})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "x.go",
		"old_string": "if ok {\n    return 1\n}", // spaces, wrong depth
		"new_string": "if ok {\n    return 2\n}"}, nil)
	if res.Error {
		t.Fatal(res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(ws, "x.go"))
	if string(b) != "package x\n\nfunc f() {\n\tif ok {\n\t\treturn 2\n\t}\n\treturn 0\n}\n" {
		t.Fatalf("not re-indented to the file:\n%s", b)
	}
	diff, _ := res.Metadata["diff"].(string)
	if res.Metadata["fuzzy"] != true || !strings.Contains(diff, "-\t\treturn 1") || !strings.Contains(diff, "+\t\treturn 2") {
		t.Fatalf("result must carry the diff: %+v", res.Metadata)
	}
}

func TestFuzzyMatchAmbiguousWritesNothing(t *testing.T) {
	body := "a {\n\tx\n}\nb {\n    x\n}\n"
	ws, tool, _ := patchEnv(t, map[string]string{"y.txt": body})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "y.txt", "old_string": "\t\tx", "new_string": "\t\tz"}, nil) // no exact match; two loose ones
	if !res.Error || !strings.Contains(res.Content, "2 matches") {
		t.Fatalf("result = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "y.txt")); string(b) != body {
		t.Fatal("an ambiguous tolerant match must write nothing")
	}
}

func TestFuzzyNeverAppliesToReplaceAll(t *testing.T) {
	_, tool, _ := patchEnv(t, map[string]string{"z.txt": "\tx\n"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "z.txt", "old_string": "  x", "new_string": "y", "replace_all": true}, nil)
	if !res.Error {
		t.Fatal("replace_all must not use the tolerant match")
	}
}

// A fuzzy edit inside edits[] reports its diff in its own result.
func TestFuzzyInMultiEdit(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"x.go": goFile})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "x.go", "edits": []any{
		map[string]any{"old_string": "return 0", "new_string": "return -1"},
		map[string]any{"old_string": "  if ok {\n      return 1\n  }", "new_string": "  if !ok {\n      return 1\n  }"},
	}}, nil)
	if res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "x.go")); !strings.Contains(string(b), "\tif !ok {\n\t\treturn 1\n\t}\n\treturn -1\n") {
		t.Fatalf("file:\n%s", b)
	}
	rs := res.Metadata["results"].([]map[string]any)
	if rs[0]["fuzzy"] != nil || rs[1]["fuzzy"] != true || !strings.Contains(rs[1]["diff"].(string), "+\tif !ok {") {
		t.Fatalf("results = %+v", rs)
	}
}

// Blank lines must stay blank; a window that would need a non-blank line to
// match a blank one does not match.
func TestFuzzyBlankLinesStayBlank(t *testing.T) {
	if _, _, n := fuzzyMatch("a\n\nb\n", "a\nx\nb"); n != 0 {
		t.Fatalf("n = %d", n)
	}
	if s, e, n := fuzzyMatch("  a\n\n  b\nc\n", "a\n\nb"); n != 1 || s != 0 || e != 9 {
		t.Fatalf("got %d %d %d", s, e, n)
	}
}

// An old_string with no line break whose loose form appears inside a line
// (not as the whole line) is not a fuzzy match: windows are whole lines.
func TestFuzzyMatchesWholeLinesOnly(t *testing.T) {
	if _, _, n := fuzzyMatch("foo bar\n", " bar"); n != 0 {
		t.Fatalf("n = %d", n)
	}
}

func TestFuzzyWithoutTrailingNewline(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"n.txt": "a\n\tb"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "n.txt", "old_string": "  b", "new_string": "  c"}, nil)
	if res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "n.txt")); string(b) != "a\n\tc" {
		t.Fatalf("file = %q", b)
	}
	if d := res.Metadata["diff"].(string); !strings.Contains(d, "-\tb\n+\tc\n") {
		t.Fatalf("diff = %q", d)
	}
}

func TestReindentDeeperLines(t *testing.T) {
	// old at base "  " (two spaces), file at base "\t"; a new line deeper
	// than any old line keeps its extra indentation relative to the base.
	got := reindent("  if x {\n      y()\n  }", "  if x {\n  }", "\tif x {\n\t}\n")
	if got != "\tif x {\n\t    y()\n\t}" {
		t.Fatalf("got %q", got)
	}
}

func TestFuzzyDeleteRemovesTheLines(t *testing.T) {
	ws, tool, _ := patchEnv(t, map[string]string{"d.txt": "a\n\tb\nc\n"})
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "d.txt", "old_string": "b", "new_string": ""}, nil); res.Error || res.Metadata["fuzzy"] == true {
		// "b" matches exactly inside "\tb": an exact edit, not fuzzy.
		if res.Error {
			t.Fatal(res.Content)
		}
	}
	ws, tool, _ = patchEnv(t, map[string]string{"d.txt": "a\n\tb x\nc\n"})
	res, _ := tool.Execute(context.Background(), map[string]any{"path": "d.txt", "old_string": "  b x\n", "new_string": ""}, nil)
	if res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "d.txt")); string(b) != "a\nc\n" {
		t.Fatalf("file = %q", b)
	}
}

func TestFuzzyRefusesBlankOldString(t *testing.T) {
	_, tool, _ := patchEnv(t, map[string]string{"e.txt": "a\n\nb\n"})
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "e.txt", "old_string": "   \n", "new_string": "x"}, nil); !res.Error {
		t.Fatalf("a blank old_string matched: %+v", res)
	}
}

// A trailing blank line in old_string is part of the window: it must match
// a blank line in the file (review: TrimRight dropped it).
func TestFuzzyKeepsTrailingBlankLines(t *testing.T) {
	if _, _, n := fuzzyMatch("  a\nb\n", "a\n\n"); n != 0 {
		t.Fatalf("a window without the blank line matched: n = %d", n)
	}
	if s, e, n := fuzzyMatch("x\n  a\n\nb\n", "a\n\n"); n != 1 || s != 2 || e != 7 {
		t.Fatalf("got %d %d %d", s, e, n)
	}
}

func TestUnifiedHunk(t *testing.T) {
	before := "a\nb\nc\nd\ne\n"
	after := "a\nb\nC\nd\ne\n"
	got := unifiedHunk("f.txt", before, after, 4, 6, 6)
	want := "--- a/f.txt\n+++ b/f.txt\n@@ -1,5 +1,5 @@\n a\n b\n-c\n+C\n d\n e\n"
	if got != want {
		t.Fatalf("hunk =\n%s\nwant\n%s", got, want)
	}
}
