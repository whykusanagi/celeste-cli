package codegraph

import "fmt"

// Confidence warning constants. These strings are stable across
// releases because callers (LLM tool users, UIs, scripts) may match
// on them directly. Add new ones freely but do NOT rename or remove
// existing ones without a version bump.
const (
	// WarnDemotedTest — result was demoted because its path matches a
	// test directory or test filename suffix.
	WarnDemotedTest = "demoted: test path"

	// WarnDemotedMock — result was demoted because its path is in a
	// mocks/, fixtures/, or stubs/ directory.
	WarnDemotedMock = "demoted: mock path"

	// WarnDemotedDeclaration — result was demoted because the symbol
	// lives in a .d.ts / .d.mts declaration-only file. Useful for TS
	// consumers: declaration files describe API surfaces but have no
	// runtime code, so a matching declaration probably isn't what you
	// want when looking for implementations.
	WarnDemotedDeclaration = "demoted: declaration-only file"

	// WarnDemotedVendored — result was demoted because the symbol is
	// in a vendored third-party directory (vendor/, node_modules/,
	// third_party/).
	WarnDemotedVendored = "demoted: vendored code"

	// WarnDemotedGenerated — result was demoted because the symbol is
	// in a build-output directory (dist/, build/, .next/, target/).
	WarnDemotedGenerated = "demoted: generated code"

	// WarnZeroEdge — the symbol has zero incoming AND zero outgoing
	// edges in the code graph. Two possible interpretations:
	//   1. Genuine dead code. Nothing calls it, it calls nothing.
	//   2. Parser limitation. The regex parser for TS/Python/Rust
	//      cannot resolve many call sites and edges for non-Go
	//      languages are systematically undercounted. An LLM should
	//      NOT conclude "dead code" from this warning alone — verify
	//      by reading the file.
	// SPEC §8.2 Issue #2 documents this ambiguity.
	WarnZeroEdge = "zero edges — may be dead code or parser limitation"

	// WarnLowConfidence — Jaccard similarity is below 0.10. Results
	// at this tier are right at the signal/noise boundary for MinHash
	// with 128 hash functions (pairwise-independent FNV variant). An
	// LLM should treat these as "maybe relevant" not "definitely
	// relevant" and verify by reading the source.
	WarnLowConfidence = "low confidence (jaccard < 0.10)"

	// WarnDeclarationOnlyType — symbol is a pure type/interface with
	// no body and zero edges. Common in TS type declaration files
	// and Go interface-only types. Probably not runtime code the
	// user wants to find.
	WarnDeclarationOnlyType = "type/interface declaration without references"
)

// computeConfidenceWarnings derives the list of confidence warnings for
// a single search result. Called at query time (no precomputation), so
// adding new warning categories requires no schema migration and no
// re-indexing.
//
// Deterministic and fast: O(len(pathFlags)) per result.
func computeConfidenceWarnings(sym Symbol, similarity float64, pathFlags []PathFlag, edgeCount int) []string {
	var warnings []string

	// 1. Path-based demotion reasons — one per flag, stable string.
	for _, f := range pathFlags {
		switch f {
		case FlagTest:
			warnings = append(warnings, WarnDemotedTest)
		case FlagMock:
			warnings = append(warnings, WarnDemotedMock)
		case FlagDeclaration:
			warnings = append(warnings, WarnDemotedDeclaration)
		case FlagVendored:
			warnings = append(warnings, WarnDemotedVendored)
		case FlagGenerated:
			warnings = append(warnings, WarnDemotedGenerated)
		}
	}

	// 2. Low-Jaccard warning. The 0.10 threshold is chosen because
	// MinHash with 128 hash functions has approximately 1/sqrt(128)
	// ≈ 8.8% standard deviation on the Jaccard estimate. Results at
	// ~0.10 similarity are within one sigma of pure noise and should
	// be treated accordingly.
	if similarity < 0.10 {
		warnings = append(warnings, WarnLowConfidence)
	}

	// 3. Zero-edge symbols. Note this is NOT necessarily dead code —
	// celeste's regex parser undercounts edges for non-Go languages
	// (SPEC §8.2 Issue #2). The warning text reflects that ambiguity
	// so LLMs don't confidently declare symbols dead.
	if edgeCount == 0 {
		warnings = append(warnings, WarnZeroEdge)
	}

	// 4. Type/interface declarations with no references. These are
	// often what users don't want — they describe an API shape but
	// aren't the implementation the search intends to find. The
	// distinction from WarnZeroEdge is that we specifically call out
	// "this is a type declaration" to give the LLM extra reasoning
	// material — maybe the user WANTS the type declaration.
	if edgeCount == 0 && isDeclarationOnlyKind(sym.Kind) {
		warnings = append(warnings, WarnDeclarationOnlyType)
	}

	return warnings
}

// isDeclarationOnlyKind returns true for symbol kinds that describe
// type shapes without runtime code. These are the kinds for which a
// zero-edge count is a signal of "declaration-only" rather than
// "dead runtime code".
func isDeclarationOnlyKind(k SymbolKind) bool {
	switch k {
	case SymbolType, SymbolInterface, SymbolStruct:
		return true
	default:
		return false
	}
}

// FormatConfidenceLine returns a human-readable one-line summary of a
// SearchResult's confidence metadata, suitable for appending to CLI /
// tool output. Empty string if there's nothing notable.
//
// Example output:
//
//	"  ⚠ demoted: mock path; zero edges — may be dead code or parser limitation; edges=0"
//	"  edges=12"
func FormatConfidenceLine(r SearchResult) string {
	if len(r.ConfidenceWarnings) == 0 {
		return fmt.Sprintf("edges=%d", r.EdgeCount)
	}
	out := ""
	for i, w := range r.ConfidenceWarnings {
		if i > 0 {
			out += "; "
		}
		out += w
	}
	return fmt.Sprintf("%s; edges=%d", out, r.EdgeCount)
}
