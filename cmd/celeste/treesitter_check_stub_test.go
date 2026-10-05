//go:build !cgo

package main

import (
	"strings"
	"testing"
)

// #376: a CGO_ENABLED=0 build falls back to the regex parsers. The release
// smoke check must notice, or a pure-Go binary could ship as a release again.
func TestTreeSitterSelfCheckFailsWithoutCgo(t *testing.T) {
	err := treeSitterSelfCheck(t.TempDir())
	if err == nil {
		t.Fatal("tree-sitter self-check passed on a CGO_ENABLED=0 build; it cannot tell the regex fallback apart")
	}
	t.Log(err)
}

// Each missing symbol is reported once, however many checks need it.
func TestTreeSitterSelfCheckReportsEachProblemOnce(t *testing.T) {
	err := treeSitterSelfCheck(t.TempDir())
	if err == nil {
		t.Fatal("self-check passed on a CGO_ENABLED=0 build")
	}
	msg := err.Error()
	_, list, _ := strings.Cut(msg, ": ")
	seen := map[string]bool{}
	for _, p := range strings.Split(list, "; ") {
		if seen[p] {
			t.Errorf("problem %q reported twice in %q", p, msg)
		}
		seen[p] = true
	}
}
