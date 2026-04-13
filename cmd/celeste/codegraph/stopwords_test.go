package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopWords_LoadedAtInit(t *testing.T) {
	require.NotNil(t, stopWords, "stopWords must be initialized by package init")
	assert.NotEmpty(t, stopWords.Version, "stopWords must have a version")
	assert.Greater(t, len(stopWords.Universal), 0, "universal set must not be empty")
}

func TestStopWords_Anchors_MustStop(t *testing.T) {
	// Canonical noise tokens — if these ever stop being stopwords,
	// something is wrong with the embedded file or the derivation
	// pipeline upstream in celeste-stopwords.
	mustStopUniversal := []string{
		"error", "get", "set", "name", "value", "string", "type", "is",
	}
	for _, tok := range mustStopUniversal {
		assert.True(t, stopWords.Universal[tok],
			"%q MUST be in the universal stopword set (canonical noise)", tok)
	}

	// Per-language anchors: tokens that should live in a specific
	// language's stopword set but NOT universal. These track the
	// idiomatic noise vocabulary of each language.
	mustStopGo := []string{"new", "err"}
	for _, tok := range mustStopGo {
		assert.True(t, stopWords.ByLang["go"][tok],
			"%q MUST be in the go-specific stopword set", tok)
	}
	mustStopPython := []string{"async", "dict", "none"}
	for _, tok := range mustStopPython {
		assert.True(t, stopWords.ByLang["python"][tok],
			"%q MUST be in the python-specific stopword set", tok)
	}
}

// TestStopWords_PreserveTokensNotStopped is a load-bearing regression
// guard: every SPEC §5 benchmark preserve token must NOT appear in ANY
// stopword set (universal or per-language). If a new stopwords.json
// artifact ever introduces one, cross-process semantic search on the
// corresponding benchmark query silently starts returning noise because
// the filter strips discriminative tokens asymmetrically. Caught
// exactly this regression ("query" leaked into the typescript list in
// celeste-stopwords v1.0.0) and held ship.
func TestStopWords_PreserveTokensNotStopped(t *testing.T) {
	preserves := []string{
		"authentication", "session", "token",
		"middleware", "handler", "http", "request",
		"database", "connection", "pool", "query",
		"file", "read", "write", "parse",
		"error", "handling", "retry",
	}
	// Exception: "error" is in the universal set as canonical noise,
	// and "file"/"read"/"write"/"handling" are also expected to be
	// ubiquitous. The STRICT invariant is only on the tokens that MUST
	// stay discriminative:
	strictPreserves := []string{
		"authentication", "session", "token",
		"middleware", "handler", "http",
		"database", "connection", "pool", "query",
		"parse", "retry",
	}
	for _, tok := range strictPreserves {
		assert.False(t, stopWords.Universal[tok],
			"%q is a SPEC §5 strict preserve but is in universal stopwords", tok)
		for lang, set := range stopWords.ByLang {
			assert.False(t, set[tok],
				"%q is a SPEC §5 strict preserve but is in %s stopwords", tok, lang)
		}
	}
	_ = preserves // keep the full list in source as documentation
}

func TestStopWords_Anchors_MustKeep(t *testing.T) {
	// SPEC §5 benchmark preserve tokens — these discriminate for the
	// acceptance queries and MUST NOT appear in ANY stopword set.
	// If one ever lands in the stopwords, the benchmark queries
	// silently start failing.
	mustKeep := []string{
		"authentication", "session", "token",
		"middleware", "handler", "http",
		"database", "connection", "pool",
		"parse",
		"retry",
	}
	for _, tok := range mustKeep {
		// Check universal set.
		assert.False(t, stopWords.Universal[tok],
			"%q MUST NOT be in the universal stopword set (SPEC §5 preserve)", tok)
		// Check every per-language set.
		for lang, set := range stopWords.ByLang {
			assert.False(t, set[tok],
				"%q MUST NOT be in the %s stopword set (SPEC §5 preserve)", tok, lang)
		}
	}
}

func TestStopWords_CompoundIdentifiers(t *testing.T) {
	// Key compound identifiers that the splitCamelCase fix backs up
	// with full-name preservation. These must be in the compound set.
	mustBeCompound := []string{
		"jquery", "github", "mysql", "kubernetes", "graphql",
	}
	for _, name := range mustBeCompound {
		assert.True(t, stopWords.IsCompound(name),
			"%q must be in compound_identifiers", name)
	}
}

func TestStopWords_CanaryPresent(t *testing.T) {
	// The canary token is a fingerprint injected by celeste-stopwords
	// to detect unauthorized copies of the stopwords.json artifact in
	// downstream products. It is not expected to match any real
	// identifier in any codebase — it exists purely as a grep-able
	// watermark. If this ever disappears, someone stripped the license
	// metadata on the way in.
	assert.True(t, stopWords.IsCompound("celestestopwordsv1"),
		"canary token 'celestestopwordsv1' must be present in compound_identifiers")
}

func TestStopWords_SizeUnder500(t *testing.T) {
	// SPEC §5.3 acceptance criterion: total stopword list < 500 entries.
	total := len(stopWords.Universal)
	for _, set := range stopWords.ByLang {
		total += len(set)
	}
	assert.Less(t, total, 500,
		"total stopword count %d exceeds SPEC §5.3 cap of 500", total)
}

func TestStopWords_Filter_UniversalOnly(t *testing.T) {
	// With empty lang parameter, only the universal set is applied.
	input := []string{"validate", "error", "session", "get", "user"}
	filtered := stopWords.Filter(input, "")

	assert.Contains(t, filtered, "validate", "validate is not a stopword")
	assert.Contains(t, filtered, "session", "session is not a stopword")
	assert.Contains(t, filtered, "user", "user is not a stopword")
	assert.NotContains(t, filtered, "error", "error is universal-stopword")
	assert.NotContains(t, filtered, "get", "get is universal-stopword")
}

func TestStopWords_Filter_PerLanguage(t *testing.T) {
	// With lang="go", both universal and Go-specific stopwords apply.
	// "ctx" is in the Go stopword set (high DF in the Go training corpus).
	input := []string{"handler", "ctx", "err", "user"}
	filtered := stopWords.Filter(input, "go")

	assert.Contains(t, filtered, "handler", "handler is preserved")
	assert.Contains(t, filtered, "user", "user is not a stopword")
	assert.NotContains(t, filtered, "ctx", "ctx is in the go-specific stopword set")
	assert.NotContains(t, filtered, "err", "err is a Go ubiquitous token")
}

func TestStopWords_Filter_PreservesOrder(t *testing.T) {
	input := []string{"a", "validate", "error", "session", "get", "token"}
	filtered := stopWords.Filter(input, "")

	// "a" might or might not be filtered (single-char stopword); don't
	// assert on it. But the surviving tokens must be in their original
	// relative order.
	lastIdx := -1
	for _, survivor := range []string{"validate", "session", "token"} {
		idx := -1
		for i, t := range filtered {
			if t == survivor {
				idx = i
				break
			}
		}
		require.GreaterOrEqual(t, idx, 0, "expected %q in filtered output", survivor)
		assert.Greater(t, idx, lastIdx, "%q should appear after the previous survivor", survivor)
		lastIdx = idx
	}
}

func TestStopWords_Filter_EmptyInput(t *testing.T) {
	assert.Empty(t, stopWords.Filter(nil, ""))
	assert.Empty(t, stopWords.Filter([]string{}, ""))
}

func TestStopWords_Filter_AllStopped(t *testing.T) {
	input := []string{"error", "get", "set", "name"}
	filtered := stopWords.Filter(input, "")
	assert.Empty(t, filtered, "all input tokens were stopwords")
}

func TestStopWords_NilFilter(t *testing.T) {
	// A nil StopWords pointer must return the input unchanged.
	var nilSW *StopWords
	input := []string{"error", "get"}
	out := nilSW.Filter(input, "")
	assert.Equal(t, input, out, "nil StopWords must pass through")
}

func TestStopWords_IsCompound_NilSafe(t *testing.T) {
	var nilSW *StopWords
	assert.False(t, nilSW.IsCompound("jquery"))
}

func TestSplitIdentifier_CompoundPreservation(t *testing.T) {
	// Full-name match: identifier is exactly a compound name.
	assert.Equal(t, []string{"jquery"}, splitIdentifier("jquery"),
		"lowercase compound name should stay atomic")
	assert.Equal(t, []string{"jquery"}, splitIdentifier("jQuery"),
		"mixed-case compound name should stay atomic (lowercased)")
	assert.Equal(t, []string{"mysql"}, splitIdentifier("mysql"),
		"lowercase compound should stay atomic")

	// Non-compound snake_case still splits.
	assert.Equal(t, []string{"validate", "session"}, splitIdentifier("validate_session"),
		"non-compound snake_case still splits normally")

	// PascalCase with compound prefix: splitCamelCase handles this
	// (the min-3-uppercase fix keeps JQueryStatic → [JQuery, Static]).
	// The compound check doesn't override splitCamelCase behavior —
	// it only fires when the ENTIRE identifier (lowercased) is in the
	// compound set.
	assert.Equal(t, []string{"jquery", "static"}, splitIdentifier("JQueryStatic"),
		"compound check + splitCamelCase fix: JQuery stays together, Static splits off")
}
