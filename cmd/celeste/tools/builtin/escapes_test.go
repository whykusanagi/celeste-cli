package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Source text whose backslash sequences are part of the code, not line breaks.
// Before #165 every one of these was corrupted on write.
var escapeSequenceSources = map[string]string{
	"go string literal": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"a\\n\\tb\")\n}\n",
	"js split":          "const lines = text.split(\"\\n\");\nconst cols = lines[0].split(\"\\t\");\n",
	"python join":       "out = \"\\t\".join(cols) + \"\\n\"\nprint(out)\n",
	"regex":             "var re = regexp.MustCompile(`\\n+\\s*`)\n",
	"escaped backslash": "path := \"C:\\\\new\\\\table\"\n",
}

// execJSON runs a tool and decodes its JSON result, failing on tool errors.
func execJSON(t *testing.T, exec func() (string, bool, error)) map[string]any {
	t.Helper()
	content, isErr, err := exec()
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if isErr {
		t.Fatalf("tool error: %s", content)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		t.Fatalf("result not JSON: %v (%s)", err, content)
	}
	return meta
}

func writeFile(t *testing.T, dir string, args map[string]any) map[string]any {
	t.Helper()
	return execJSON(t, func() (string, bool, error) {
		res, err := NewWriteFileTool(dir).Execute(context.Background(), args, nil)
		return res.Content, res.Error, err
	})
}

func patchFile(t *testing.T, dir string, args map[string]any) map[string]any {
	t.Helper()
	return execJSON(t, func() (string, bool, error) {
		res, err := NewPatchFileTool(dir).Execute(context.Background(), args, nil)
		return res.Content, res.Error, err
	})
}

func TestDecodeDoubleEscaped(t *testing.T) {
	tests := []struct {
		name, in, want string
		decoded        bool
	}{
		{"real newlines kept", "a\\nb\nc\n", "a\\nb\nc\n", false},
		{"no escapes", "plain", "plain", false},
		{"lone tab escape kept", `split("\t")`, `split("\t")`, false},
		{"double-escaped lines", `line1\nline2\n`, "line1\nline2\n", true},
		{"double-escaped tab", `func a() {\n\treturn 1\n}`, "func a() {\n\treturn 1\n}", true},
		{"double-escaped literal", `fmt.Println(\"a\\n\")\n`, "fmt.Println(\"a\\n\")\n", true},
		{"invalid json body falls back", `say "hi"\nbye`, "say \"hi\"\nbye", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, decoded := decodeDoubleEscaped(tt.in)
			if got != tt.want || decoded != tt.decoded {
				t.Fatalf("decodeDoubleEscaped(%q) = %q, %v; want %q, %v", tt.in, got, decoded, tt.want, tt.decoded)
			}
		})
	}
}

func TestWriteFile_PreservesEscapeSequences(t *testing.T) {
	for name, src := range escapeSequenceSources {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			meta := writeFile(t, dir, map[string]any{"path": "f.txt", "content": src})
			if got := mustRead(t, filepath.Join(dir, "f.txt")); got != src {
				t.Fatalf("write_file changed content:\n got %q\nwant %q", got, src)
			}
			if _, ok := meta["decoded_escapes"]; ok {
				t.Fatalf("decoded_escapes set for content with real newlines")
			}
		})
	}
}

func TestWriteFile_DecodesDoubleEscapedPayload(t *testing.T) {
	dir := t.TempDir()
	// What arrives when a model double-escapes the file from escapeSequenceSources["go string literal"].
	meta := writeFile(t, dir, map[string]any{
		"path":    "main.go",
		"content": `package main\n\nfunc main() {\n\tprintln(\"a\\n\")\n}\n`,
	})
	want := "package main\n\nfunc main() {\n\tprintln(\"a\\n\")\n}\n"
	if got := mustRead(t, filepath.Join(dir, "main.go")); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if meta["decoded_escapes"] != true {
		t.Fatalf("expected decoded_escapes=true, got %v", meta)
	}
}

func TestPatchFile_PreservesEscapeSequences(t *testing.T) {
	dir := t.TempDir()
	src := escapeSequenceSources["go string literal"]
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644)

	// old_string copied verbatim from the file, including its \n escape.
	meta := patchFile(t, dir, map[string]any{
		"path":       "main.go",
		"old_string": `fmt.Println("a\n\tb")`,
		"new_string": `fmt.Println("c\n")`,
	})
	want := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"c\\n\")\n}\n"
	if got := mustRead(t, filepath.Join(dir, "main.go")); got != want {
		t.Fatalf("patch_file corrupted escapes:\n got %q\nwant %q", got, want)
	}
	if _, ok := meta["decoded_escapes"]; ok {
		t.Fatalf("decoded_escapes set for a verbatim match")
	}
	if meta["note"] == nil {
		t.Fatalf("expected a note that backslash sequences were kept, got %v", meta)
	}
}

func TestPatchFile_DoubleEscapedCall(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("func a() int {\n\treturn 1\n}\n"), 0644)

	meta := patchFile(t, dir, map[string]any{
		"path":       "a.go",
		"old_string": `func a() int {\n\treturn 1\n}`,
		"new_string": `func a() int {\n\treturn 2\n}`,
	})
	if got := mustRead(t, filepath.Join(dir, "a.go")); got != "func a() int {\n\treturn 2\n}\n" {
		t.Fatalf("got %q", got)
	}
	if meta["decoded_escapes"] != true {
		t.Fatalf("expected decoded_escapes=true, got %v", meta)
	}
}

func TestSplice_AnchorWithEscapeSequenceInSource(t *testing.T) {
	dir := t.TempDir()
	src := "keep\nsep := \"\\n\"\nbody\nend := \"\\t\"\nrest\n"
	os.WriteFile(filepath.Join(dir, "f.go"), []byte(src), 0644)

	_, errStr := splice(t, dir, map[string]any{
		"op": "move", "source": "f.go", "dest": "g.go",
		"start_anchor": `sep := "\n"`, "end_anchor": `end := "\t"`,
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	if got, want := mustRead(t, filepath.Join(dir, "g.go")), "sep := \"\\n\"\nbody\nend := \"\\t\"\n"; got != want {
		t.Fatalf("moved region = %q want %q", got, want)
	}
	if got, want := mustRead(t, filepath.Join(dir, "f.go")), "keep\nrest\n"; got != want {
		t.Fatalf("source after move = %q want %q", got, want)
	}
}

func TestSplice_DoubleEscapedMultiLineAnchor(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("A\nB\nC\nD\n"), 0644)

	_, errStr := splice(t, dir, map[string]any{
		"op": "copy", "source": "f.txt", "dest": "g.txt",
		"start_anchor": `A\nB`, "end_anchor": "C",
	})
	if errStr != "" {
		t.Fatalf("splice failed: %s", errStr)
	}
	if got := mustRead(t, filepath.Join(dir, "g.txt")); got != "A\nB\nC\n" {
		t.Fatalf("copied region = %q", got)
	}
}
