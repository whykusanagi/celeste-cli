package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden files are compared byte for byte, so a Windows checkout must not
// turn their line endings into CRLF (#338, #345): .gitattributes pins them
// to LF, and every golden file under cmd is LF-only.
func TestGoldenFilesKeepLF(t *testing.T) {
	attrs, err := os.ReadFile(filepath.Join("..", "..", ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	pinned := false
	for _, line := range strings.Split(string(attrs), "\n") {
		if strings.Join(strings.Fields(line), " ") == "*.golden text eol=lf" {
			pinned = true
		}
	}
	if !pinned {
		t.Error(".gitattributes does not pin *.golden to LF (want `*.golden text eol=lf`)")
	}
	n := 0
	err = filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".golden") {
			return err
		}
		n++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte("\r\n")) {
			t.Errorf("%s has CRLF line endings", filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("found no golden files under cmd")
	}
}
