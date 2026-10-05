package main

import "testing"

// #376: release binaries carry the tree-sitter parsers now. The docs say so,
// say what a CGO_ENABLED=0 source build gives up, and stop claiming the
// project avoids CGo.
func TestDocsSayReleaseBinariesCarryTreeSitter(t *testing.T) {
	langs := "TypeScript, PHP, Python, Rust, Java, C/C++ and Ruby"
	cg := repoDoc(t, "docs/CODEGRAPH.md")
	requireAll(t, "docs/CODEGRAPH.md", cg, langs, "CGO_ENABLED=0", "regex", "celeste index selfcheck")
	requireNone(t, "docs/CODEGRAPH.md", cg, "which the project avoids")
	for _, name := range []string{"README.md", "docs/CAPABILITIES.md"} {
		doc := repoDoc(t, name)
		requireAll(t, name, doc, langs, "CGO_ENABLED=0")
		requireNone(t, name, doc, "tree-sitter TypeScript pars")
	}
	requireNone(t, "docs/ROADMAP.md", repoDoc(t, "docs/ROADMAP.md"), "today's release builds are pure Go")
	requireAll(t, "MIGRATING-2.0.md", repoDoc(t, "MIGRATING-2.0.md"), "tree-sitter", "statically linked", "glibc")
	requireAll(t, "RELEASE_CHECKLIST.md", repoDoc(t, "RELEASE_CHECKLIST.md"), "dry run")
}
