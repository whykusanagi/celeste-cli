//go:build !cgo

package main

import "testing"

// #376: a CGO_ENABLED=0 build falls back to the regex parsers. The release
// smoke check must notice, or a pure-Go binary could ship as a release again.
func TestTreeSitterSelfCheckFailsWithoutCgo(t *testing.T) {
	err := treeSitterSelfCheck(t.TempDir())
	if err == nil {
		t.Fatal("tree-sitter self-check passed on a CGO_ENABLED=0 build; it cannot tell the regex fallback apart")
	}
	t.Log(err)
}
