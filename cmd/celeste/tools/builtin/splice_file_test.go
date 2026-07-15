package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// splice runs the tool and returns the decoded result metadata (or the error string).
func splice(t *testing.T, dir string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := NewSpliceFileTool(dir).Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if res.Error {
		return nil, res.Content
	}
	var meta map[string]any
	if uerr := json.Unmarshal([]byte(res.Content), &meta); uerr != nil {
		t.Fatalf("result not JSON: %v (%s)", uerr, res.Content)
	}
	return meta, ""
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func bigCSS(bytes int) string {
	var b strings.Builder
	i := 0
	for b.Len() < bytes {
		b.WriteString(".sel-")
		b.WriteString(strings.Repeat("x", 8))
		b.WriteString(" { color: #abcdef; margin: 0; }\n")
		i++
	}
	return b.String()
}

// T1: extract a ~115 KB inline CSS block from HTML into an external file (the
// incident case). Cross-file MOVE by anchors. Bytes are moved on disk, never
// regenerated; the result reports routed_through_llm=false.
func TestSplice_T1_ExtractInlineCSSToFile(t *testing.T) {
	dir := t.TempDir()
	css := bigCSS(115 * 1024)
	html := "<html><head>\n<style id=\"theme\">\n/* theme:start */\n" + css + "/* theme:end */\n</style>\n</head><body>hi</body></html>\n"
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		t.Fatal(err)
	}

	meta, errStr := splice(t, dir, map[string]any{
		"op": "move", "source": "index.html", "dest": "theme.css",
		"start_anchor": "/* theme:start */", "end_anchor": "/* theme:end */",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	if meta["routed_through_llm"] != false {
		t.Fatal("bytes must not be routed through the model")
	}

	gotSrc := mustRead(t, htmlPath)
	gotDest := mustRead(t, filepath.Join(dir, "theme.css"))

	if strings.Contains(gotSrc, "/* theme:start */") || strings.Contains(gotSrc, css) {
		t.Fatal("CSS block still present in source after move")
	}
	if !strings.Contains(gotDest, css) {
		t.Fatal("CSS block not found in dest after move")
	}
	// Byte-exact: the dest holds the markers + full css, untouched.
	if !strings.Contains(gotDest, "/* theme:start */\n"+css+"/* theme:end */") {
		t.Fatal("extracted block was not byte-identical")
	}
}

// T2: COPY a line range into a dest, inserted after an anchor. Source unchanged.
func TestSplice_T2_CopyLineRangeAfterAnchor(t *testing.T) {
	dir := t.TempDir()
	src := "L1\nL2\nL3\nL4\nL5\n"
	dst := "top\nMARKER\nbottom\n"
	os.WriteFile(filepath.Join(dir, "src.txt"), []byte(src), 0644)
	os.WriteFile(filepath.Join(dir, "dst.txt"), []byte(dst), 0644)

	_, errStr := splice(t, dir, map[string]any{
		"op": "copy", "source": "src.txt", "dest": "dst.txt",
		"start_line": 2, "end_line": 3,
		"dest_anchor": "MARKER", "position": "after",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	if got := mustRead(t, filepath.Join(dir, "src.txt")); got != src {
		t.Fatalf("copy must not modify source; got %q", got)
	}
	want := "top\nMARKER\nL2\nL3\nbottom\n"
	if got := mustRead(t, filepath.Join(dir, "dst.txt")); got != want {
		t.Fatalf("dest = %q want %q", got, want)
	}
}

// T3: REPLACE a region between two dest anchors with a source region.
func TestSplice_T3_ReplaceBetweenDestAnchors(t *testing.T) {
	dir := t.TempDir()
	src := "NEW-A\nNEW-B\n"
	dst := "keep1\nBEGIN\nold junk\nmore junk\nEND\nkeep2\n"
	os.WriteFile(filepath.Join(dir, "src.txt"), []byte(src), 0644)
	os.WriteFile(filepath.Join(dir, "dst.txt"), []byte(dst), 0644)

	_, errStr := splice(t, dir, map[string]any{
		"op": "copy", "source": "src.txt", "dest": "dst.txt",
		"start_line": 1, "end_line": 2,
		"dest_replace_start": "BEGIN", "dest_replace_end": "END",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	want := "keep1\nNEW-A\nNEW-B\nkeep2\n"
	if got := mustRead(t, filepath.Join(dir, "dst.txt")); got != want {
		t.Fatalf("dest = %q want %q", got, want)
	}
}

// T4: byte integrity at 478 KB — the spliced bytes equal the source region
// exactly, proving no corruption on a payload far larger than any model arg.
func TestSplice_T4_ByteIntegrityLargePayload(t *testing.T) {
	dir := t.TempDir()
	block := bigCSS(478 * 1024)
	src := "HEAD\n/* s */\n" + block + "/* e */\nTAIL\n"
	os.WriteFile(filepath.Join(dir, "src.txt"), []byte(src), 0644)
	os.WriteFile(filepath.Join(dir, "dst.txt"), []byte("x\n"), 0644)

	meta, errStr := splice(t, dir, map[string]any{
		"op": "move", "source": "src.txt", "dest": "dst.txt",
		"start_anchor": "/* s */", "end_anchor": "/* e */",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	dst := mustRead(t, filepath.Join(dir, "dst.txt"))
	if !strings.Contains(dst, block) {
		t.Fatal("478KB block corrupted or truncated in transit")
	}
	if bm, _ := meta["bytes_moved"].(float64); int(bm) < 478*1024 {
		t.Fatalf("bytes_moved=%v, expected >= 478KB", meta["bytes_moved"])
	}
}

// T5: error handling — missing anchor, ambiguous anchor, bad range.
func TestSplice_T5_Errors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "src.txt"), []byte("dup\na\ndup\n"), 0644)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing anchor", map[string]any{"op": "move", "source": "src.txt", "dest": "d.txt", "start_anchor": "nope", "end_anchor": "a"}, "not found"},
		{"ambiguous start", map[string]any{"op": "move", "source": "src.txt", "dest": "d.txt", "start_anchor": "dup", "end_anchor": "dup"}, "appears 2 times"},
		{"bad range", map[string]any{"op": "copy", "source": "src.txt", "dest": "d.txt", "start_line": 5, "end_line": 9}, "outside the source"},
		{"bad op", map[string]any{"op": "delete", "source": "src.txt"}, `op must be "copy" or "move"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errStr := splice(t, dir, c.args)
			if errStr == "" || !strings.Contains(errStr, c.want) {
				t.Fatalf("error=%q, want it to contain %q", errStr, c.want)
			}
		})
	}
}

// Intra-file move: relocate a block within one file (dest defaults to source).
func TestSplice_IntraFileMove(t *testing.T) {
	dir := t.TempDir()
	src := "A\nB\n<<MOVE>>\nC\n<<END>>\nD\nHERE\nE\n"
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte(src), 0644)
	_, errStr := splice(t, dir, map[string]any{
		"op": "move", "source": "f.txt",
		"start_anchor": "<<MOVE>>", "end_anchor": "<<END>>",
		"dest_anchor": "HERE", "position": "after",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	want := "A\nB\nD\nHERE\n<<MOVE>>\nC\n<<END>>\nE\n"
	if got := mustRead(t, filepath.Join(dir, "f.txt")); got != want {
		t.Fatalf("intra-file move = %q want %q", got, want)
	}
}

// The patch_file guard steers oversized literals to splice_file.
func TestPatchFile_OversizedLiteralRoutesToSplice(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("marker\n"), 0644)
	res, err := NewPatchFileTool(dir).Execute(context.Background(), map[string]any{
		"path": "f.txt", "old_string": "marker", "new_string": strings.Repeat("z", 17*1024),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Error || !strings.Contains(res.Content, "splice_file") {
		t.Fatalf("expected oversized literal to be rejected with a splice_file hint, got error=%v content=%q", res.Error, res.Content)
	}
}
