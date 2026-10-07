package main

import "testing"

// #381: Go turns CGo off by itself only when CGO_ENABLED and CC are unset
// and no default C compiler exists; an explicit CGO_ENABLED=1 fails the
// build instead of falling back. The docs that promise the regex fallback
// say so.
func TestDocsQualifyTheNoCompilerFallback(t *testing.T) {
	for _, name := range []string{"docs/CODEGRAPH.md", "README.md", "MIGRATING-2.0.md"} {
		doc := repoDoc(t, name)
		requireAll(t, name, doc, "`CGO_ENABLED=1`", "`CC`")
		requireNone(t, name, doc, "or on a machine without a C compiler", "Without one,\nor with", "or\nwithout a C compiler")
	}
	// Release binaries are CGo builds since 2.0; the Go pass analyses the
	// CGO_ENABLED=0 build for another reason.
	requireNone(t, "docs/CODEGRAPH.md", repoDoc(t, "docs/CODEGRAPH.md"), "(what release binaries are)")
	requireNone(t, "codegraph/goload.go", repoDoc(t, "cmd/celeste/codegraph/goload.go"), "which is what release binaries")
}
