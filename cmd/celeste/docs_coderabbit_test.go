package main

import (
	"strings"
	"testing"
)

// #381: with CGO_ENABLED unset, Go turns CGo off by itself when CC is also
// unset and no default C compiler exists, or when the build cross-compiles;
// an explicit CGO_ENABLED=1 fails the build instead of falling back. The
// docs that promise the regex fallback say so.
func TestDocsQualifyTheNoCompilerFallback(t *testing.T) {
	for _, name := range []string{"docs/CODEGRAPH.md", "README.md", "MIGRATING-2.0.md"} {
		doc := repoDoc(t, name)
		requireAll(t, name, doc, "`CGO_ENABLED=1`", "`CC`", "cross-compile")
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

// README's provider list says the same as COMPARISON.md: a local server may
// be on another machine on the LAN.
func TestReadmeLocalProviderCoversLAN(t *testing.T) {
	const name = "README.md"
	line := ""
	for _, l := range strings.Split(repoDoc(t, name), "\n") {
		if strings.Contains(l, "**Local** (mlx-vlm") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("%s has no Local provider bullet", name)
	}
	requireNone(t, name+" (Local provider)", line, "server on localhost")
	requireAll(t, name+" (Local provider)", line, "local network")
}

// docParagraph returns the blank-line-separated paragraph of doc that
// contains marker, with its line breaks turned into spaces.
func docParagraph(t *testing.T, name, doc, marker string) string {
	t.Helper()
	for _, p := range strings.Split(doc, "\n\n") {
		if strings.Contains(strings.ReplaceAll(p, "\n", " "), marker) {
			return strings.ReplaceAll(p, "\n", " ")
		}
	}
	t.Fatalf("%s has no paragraph containing %q", name, marker)
	return ""
}

// The LLM provider guide and the changelog describe the same
// providers.IsLocalHost rule as MIGRATING-2.0.md, so they list every
// category it takes, `.localhost` names and unspecified addresses included.
func TestLocalHostListsAreComplete(t *testing.T) {
	const guide = "docs/LLM_PROVIDERS.md"
	doc := repoDoc(t, guide)
	for _, marker := range []string{"detects as the **local** provider", "gets **600 s**"} {
		p := docParagraph(t, guide, doc, marker)
		requireAll(t, guide+" ("+marker+")", p,
			"`localhost`", "`.localhost`", "loopback", "private", "link-local", "unspecified",
			"single-label", "`.local`", "`.lan`", "`.internal`", "`.home.arpa`")
	}
	const cl = "CHANGELOG.md"
	log := repoDoc(t, cl)
	for _, marker := range []string{"one rule decides whether a server is local", "left at the 60 s default gets 600 s"} {
		entry := ""
		for _, l := range strings.Split(log, "\n") {
			if strings.Contains(l, marker) {
				entry = l
				break
			}
		}
		if entry == "" {
			t.Fatalf("%s has no entry containing %q", cl, marker)
		}
		requireAll(t, cl+" ("+marker+")", entry,
			"`localhost`", "`.localhost`", "loopback", "private", "link-local", "unspecified",
			"single-label", "`.local`", "`.lan`", "`.internal`", "`.home.arpa`")
	}
}
