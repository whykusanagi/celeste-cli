//go:build cgo

package main

import "testing"

// #376: a CGo build carries the tree-sitter parsers, so the release smoke
// check (celeste index selfcheck) must pass on it.
func TestTreeSitterSelfCheckPassesWithCgo(t *testing.T) {
	if err := treeSitterSelfCheck(t.TempDir()); err != nil {
		t.Fatalf("tree-sitter self-check failed on a CGo build: %v", err)
	}
}

// The release smoke step runs `celeste index selfcheck` and needs exit 0.
func TestIndexSelfCheckCommandSucceedsWithCgo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if code := runIndexSelfCheck(); code != 0 {
		t.Fatalf("runIndexSelfCheck() = %d, want 0", code)
	}
}
