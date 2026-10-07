package main

import (
	"strings"
	"testing"
)

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

// #383: the 600 s local timeout follows providers.IsLocalHost, so the
// migration guide lists every host it treats as local.
func TestMigratingListsEveryLocalHostCategory(t *testing.T) {
	const name = "MIGRATING-2.0.md"
	doc := repoDoc(t, name)
	row := ""
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "|") && strings.Contains(line, "gets 600 s") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("%s has no row for the 600 s local timeout", name)
	}
	requireAll(t, name+" (600 s row)", row,
		"`localhost`", "`.localhost`", "loopback", "private", "link-local", "unspecified",
		"single-label", "`.local`", "`.lan`", "`.internal`", "`.home.arpa`")
}

// #387: Update skips a file whose content hash matches its record, so an
// index whose file records survive without their symbols is repaired only
// by a rebuild. The store comment and CODEGRAPH.md send users there.
func TestCodegraphRecoveryPointsAtRebuild(t *testing.T) {
	store := repoDoc(t, "cmd/celeste/codegraph/store.go")
	requireNone(t, "codegraph/store.go", store, "the next update re-indexes what is missing")
	requireAll(t, "codegraph/store.go", store, "`celeste index rebuild`", "file whose content hash matches its record")
	cg := repoDoc(t, "docs/CODEGRAPH.md")
	requireAll(t, "docs/CODEGRAPH.md", cg, "`celeste index rebuild`", "plain `celeste index` only updates", "skips every file whose content")
	requireNone(t, "docs/CODEGRAPH.md", cg, "(`celeste index` only updates)")
}

// The local provider is decided by the host (providers.IsLocalHost), which
// takes LAN servers too, so COMPARISON.md's local-models row does not stop
// at localhost.
func TestComparisonLocalModelsRowCoversLAN(t *testing.T) {
	const name = "docs/COMPARISON.md"
	row := ""
	for _, line := range strings.Split(repoDoc(t, name), "\n") {
		if strings.HasPrefix(line, "| **Local models**") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("%s has no Local models row", name)
	}
	requireNone(t, name+" (Local models row)", row, "server on localhost")
	requireAll(t, name+" (Local models row)", row, "local network")
}
