package codegraph

import (
	"strings"
)

// PathFlag is a machine-readable marker attached to a search result when
// the symbol's file path matches a known pattern that affects its
// interpretation — test fixture, mock, type declaration, vendored code,
// build output, etc.
//
// Flags are computed at query time (not stored in the index), so adding
// new flag categories does not invalidate existing codegraph databases.
// Callers can read SearchResult.PathFlags to understand WHY a symbol was
// demoted from the "clean" ranking tier.
type PathFlag string

const (
	// FlagTest — symbol lives in a test file or test directory. These
	// are genuine test helpers: TestFoo functions in Go's _test.go files,
	// tests/*.py in Python, *.spec.ts / *.test.ts in TypeScript, and so on.
	FlagTest PathFlag = "test"

	// FlagMock — symbol is in a mocks/, fixtures/, or stubs/ directory.
	// Mock handlers, fake services, test doubles. These pollute queries
	// like "http request handler middleware" because they share
	// discriminative tokens with production middleware without BEING
	// production middleware. Q2 in the grafana A/B test was 100% mock
	// handlers for exactly this reason.
	FlagMock PathFlag = "mock"

	// FlagDeclaration — symbol is in a pure type declaration file
	// (e.g. TypeScript .d.ts). These describe an API surface but have
	// no runtime code. Usually undesirable as a semantic search match
	// because the user is looking for implementations, not declarations.
	// JQueryStatic lives in a .d.ts file and this flag would demote it
	// even without the splitCamelCase fix.
	FlagDeclaration PathFlag = "declaration"

	// FlagVendored — symbol is in a vendored dependency or third-party
	// package directory (vendor/, node_modules/, bower_components/).
	// These are external code the user didn't write. Usually irrelevant.
	FlagVendored PathFlag = "vendored"

	// FlagGenerated — symbol is in a generated-code output directory
	// (dist/, build/, .next/, out/, target/). Post-compile artifacts,
	// transpiled output, build caches. Never what the user wants.
	FlagGenerated PathFlag = "generated"
)

// All path patterns are matched against the NORMALIZED forward-slash
// version of the file path — we split on '/' and look for exact
// directory-component matches or specific suffixes. No regex, no glob
// matching. Keeps the classifier ~O(path length) per symbol and trivial
// to audit.
var (
	// testDirNames are exact path components that indicate a test dir.
	testDirNames = map[string]bool{
		"test":           true,
		"tests":          true,
		"__tests__":      true,
		"__test__":       true,
		"spec":           true,
		"specs":          true,
		"e2e":            true,
		"e2e-playwright": true,
	}

	// mockDirNames are exact path components that indicate mock/fixture dirs.
	mockDirNames = map[string]bool{
		"mocks":     true,
		"mock":      true,
		"__mocks__": true,
		"fixtures":  true,
		"fixture":   true,
		"stubs":     true,
		"stub":      true,
		"fakes":     true,
		"fake":      true,
	}

	// vendoredDirNames are exact path components that indicate third-party code.
	vendoredDirNames = map[string]bool{
		"vendor":           true,
		"node_modules":     true,
		"bower_components": true,
		"third_party":      true,
		"third-party":      true,
		".venv":            true,
		"venv":             true,
		"site-packages":    true,
	}

	// generatedDirNames are exact path components that indicate build output.
	generatedDirNames = map[string]bool{
		"dist":        true,
		"build":       true,
		"out":         true,
		".next":       true,
		".nuxt":       true,
		"target":      true,
		"__pycache__": true,
		".cache":      true,
	}

	// testFileSuffixes match the END of a file path (filename only).
	testFileSuffixes = []string{
		"_test.go", // Go test convention
		".test.ts", // JS/TS test file
		".test.tsx",
		".test.js",
		".test.jsx",
		".spec.ts", // JS/TS spec file
		".spec.tsx",
		".spec.js",
		".spec.jsx",
		"_test.py", // Python pytest convention
		"_spec.rb", // Ruby rspec convention
	}
)

// ClassifyPath inspects a file path and returns the set of PathFlags that
// apply. Empty result means the symbol is in a "clean" path with no
// demotion warranted.
//
// Deterministic, fast (O(path length)), and order-independent: the same
// path always produces the same flag set.
func ClassifyPath(path string) []PathFlag {
	if path == "" {
		return nil
	}

	// Normalize separators so Windows-style paths work.
	normalized := strings.ReplaceAll(path, "\\", "/")

	var flags []PathFlag
	has := make(map[PathFlag]bool)
	addFlag := func(f PathFlag) {
		if !has[f] {
			has[f] = true
			flags = append(flags, f)
		}
	}

	// 1. Directory component matching — walk each path segment once.
	segments := strings.Split(normalized, "/")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		// Note: a file named "test.go" (not _test.go) does NOT match
		// the test dir check because the final segment is the filename
		// and "test" != "test.go". We treat dir names as exact-match only.
		if testDirNames[seg] {
			addFlag(FlagTest)
		}
		if mockDirNames[seg] {
			addFlag(FlagMock)
		}
		if vendoredDirNames[seg] {
			addFlag(FlagVendored)
		}
		if generatedDirNames[seg] {
			addFlag(FlagGenerated)
		}
	}

	// 2. Filename suffix matching — the last segment only.
	if len(segments) > 0 {
		filename := segments[len(segments)-1]
		for _, suffix := range testFileSuffixes {
			if strings.HasSuffix(filename, suffix) {
				addFlag(FlagTest)
				break
			}
		}
		// TypeScript .d.ts declaration files.
		if strings.HasSuffix(filename, ".d.ts") || strings.HasSuffix(filename, ".d.mts") {
			addFlag(FlagDeclaration)
		}
	}

	return flags
}

// IsDemotable returns true if the flag set is non-empty — i.e., at least
// one demotion reason applies. Pure convenience helper.
func IsDemotable(flags []PathFlag) bool {
	return len(flags) > 0
}

// PathFlagStrings converts a []PathFlag to a []string for serialization
// to JSON / API responses / logs.
func PathFlagStrings(flags []PathFlag) []string {
	if len(flags) == 0 {
		return nil
	}
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = string(f)
	}
	return out
}

// queryWantsTests returns true if the user's raw query contains tokens
// that suggest they're actually looking for test code. If so, the path
// filter should NOT demote test/mock results — demoting them would hide
// exactly what the user asked for.
//
// Simple substring check on lowercased query. Recognized intent tokens:
// "test", "tests", "mock", "mocks", "fake", "stub", "fixture", "spec",
// "e2e", "assert", "expect".
func queryWantsTests(query string) bool {
	q := strings.ToLower(query)
	// Fast-path: most queries are 2-5 tokens; Contains is cheap here.
	intentTokens := []string{
		"test", "tests", "testing",
		"mock", "mocks", "mocking",
		"fake", "fakes",
		"stub", "stubs",
		"fixture", "fixtures",
		"spec", "specs",
		"assert", "asserts",
		"expect", "expects",
	}
	for _, t := range intentTokens {
		// Use space-padded comparison to avoid "bestInterface" matching "test".
		padded := " " + q + " "
		if strings.Contains(padded, " "+t+" ") {
			return true
		}
	}
	return false
}
