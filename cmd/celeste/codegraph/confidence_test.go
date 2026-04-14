package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeConfidenceWarnings_CleanHighConfidenceResult(t *testing.T) {
	sym := Symbol{Name: "MySQLQuery", Kind: SymbolStruct, File: "pkg/db/query.go"}
	warnings := computeConfidenceWarnings(sym, 0.25, nil, 8)
	assert.Empty(t, warnings, "clean high-confidence result with edges should have no warnings")
}

func TestComputeConfidenceWarnings_PathFlagsProduceDemotionWarnings(t *testing.T) {
	sym := Symbol{Name: "exportSpecificMuteTimingsHandler", Kind: SymbolFunction,
		File: "features/alerting/unified/mocks/server/handlers/provisioning.ts"}
	warnings := computeConfidenceWarnings(sym, 0.16, []PathFlag{FlagMock}, 3)

	assert.Contains(t, warnings, WarnDemotedMock)
	assert.NotContains(t, warnings, WarnZeroEdge)
	assert.NotContains(t, warnings, WarnLowConfidence)
}

func TestComputeConfidenceWarnings_LowSimilarity(t *testing.T) {
	sym := Symbol{Name: "something", Kind: SymbolFunction, File: "src/mod.go"}
	warnings := computeConfidenceWarnings(sym, 0.08, nil, 5)
	assert.Contains(t, warnings, WarnLowConfidence)
}

func TestComputeConfidenceWarnings_ZeroEdgeFunction(t *testing.T) {
	// A function with zero edges is suspicious but NOT tagged as
	// "declaration only" because functions aren't declarations.
	sym := Symbol{Name: "orphanFunc", Kind: SymbolFunction, File: "src/mod.go"}
	warnings := computeConfidenceWarnings(sym, 0.20, nil, 0)
	assert.Contains(t, warnings, WarnZeroEdge)
	assert.NotContains(t, warnings, WarnDeclarationOnlyType)
}

func TestComputeConfidenceWarnings_ZeroEdgeType(t *testing.T) {
	// A type/interface/struct with zero edges gets BOTH warnings:
	// zero-edge (general) AND declaration-only (specific).
	cases := []SymbolKind{SymbolType, SymbolInterface, SymbolStruct}
	for _, k := range cases {
		t.Run(string(k), func(t *testing.T) {
			sym := Symbol{Name: "Something", Kind: k, File: "src/types.go"}
			warnings := computeConfidenceWarnings(sym, 0.20, nil, 0)
			assert.Contains(t, warnings, WarnZeroEdge)
			assert.Contains(t, warnings, WarnDeclarationOnlyType)
		})
	}
}

func TestComputeConfidenceWarnings_JQueryStaticStyleResult(t *testing.T) {
	// The exact SPEC §8.1 Issue #1 symbol: JQueryStatic in a .d.ts file.
	// Pre-v1.9.0 this was the #2 result for Q3 "database connection
	// pool query" on grafana. Post-v1.9.0 it's gone from the top-10
	// because of splitCamelCase, but if it ever surfaces again the
	// warnings should tell the LLM exactly why to distrust it:
	//
	//   - demoted: declaration-only file (because .d.ts)
	//   - zero edges — may be dead code or parser limitation (because
	//     the regex parser doesn't resolve interface usage)
	//   - type/interface declaration without references
	sym := Symbol{
		Name: "JQueryStatic",
		Kind: SymbolInterface,
		File: "types/jquery/jquery.d.ts",
	}
	flags := ClassifyPath(sym.File)
	warnings := computeConfidenceWarnings(sym, 0.14, flags, 0)

	assert.Contains(t, warnings, WarnDemotedDeclaration)
	assert.Contains(t, warnings, WarnZeroEdge)
	assert.Contains(t, warnings, WarnDeclarationOnlyType)
	// A careful LLM reading these three warnings together should
	// conclude: "don't trust this as a real database match."
}

func TestComputeConfidenceWarnings_MultipleFlags(t *testing.T) {
	sym := Symbol{Name: "MockX", Kind: SymbolFunction, File: "dist/tests/mock.test.js"}
	flags := ClassifyPath(sym.File)
	warnings := computeConfidenceWarnings(sym, 0.15, flags, 1)

	// Multiple demotion reasons compound — tests + generated + test-suffix.
	assert.Contains(t, warnings, WarnDemotedTest)
	assert.Contains(t, warnings, WarnDemotedGenerated)
}

func TestIsDeclarationOnlyKind(t *testing.T) {
	assert.True(t, isDeclarationOnlyKind(SymbolType))
	assert.True(t, isDeclarationOnlyKind(SymbolInterface))
	assert.True(t, isDeclarationOnlyKind(SymbolStruct))

	assert.False(t, isDeclarationOnlyKind(SymbolFunction))
	assert.False(t, isDeclarationOnlyKind(SymbolMethod))
	assert.False(t, isDeclarationOnlyKind(SymbolConst))
	assert.False(t, isDeclarationOnlyKind(SymbolVar))
}

func TestFormatConfidenceLine(t *testing.T) {
	// Clean result — just the edge count.
	r1 := SearchResult{EdgeCount: 12}
	assert.Equal(t, "edges=12", FormatConfidenceLine(r1))

	// Result with one warning.
	r2 := SearchResult{
		ConfidenceWarnings: []string{WarnLowConfidence},
		EdgeCount:          3,
	}
	assert.Equal(t, "low confidence (jaccard < 0.10); edges=3", FormatConfidenceLine(r2))

	// Result with multiple warnings — semicolon joined.
	r3 := SearchResult{
		ConfidenceWarnings: []string{WarnDemotedMock, WarnZeroEdge},
		EdgeCount:          0,
	}
	line := FormatConfidenceLine(r3)
	assert.Contains(t, line, "demoted: mock path")
	assert.Contains(t, line, "zero edges")
	assert.Contains(t, line, "edges=0")
}

// TestSemanticSearch_PopulatesConfidenceFields is the integration test
// for the confidence metadata pipeline end-to-end: build an index,
// run a search, verify every result has PathFlags, EdgeCount, and
// ConfidenceWarnings populated consistently with its file path and
// edge structure.
func TestSemanticSearch_PopulatesConfidenceFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module confidence_test\n\ngo 1.26\n")

	// Real production code with a proper call graph — should get
	// clean, high-confidence results with populated edges.
	writeFile(t, dir, "pkg/server/handler.go", `package server

type Request struct { URL string }
type Response struct { Status int }

func HandleDatabaseQuery(req *Request) *Response {
	resp := &Response{Status: 200}
	return resp
}

func HandleAuthToken(req *Request) *Response {
	return HandleDatabaseQuery(req)
}
`)

	// Test file — should get WarnDemotedTest.
	writeFile(t, dir, "pkg/server/handler_test.go", `package server

func TestHandleDatabaseQuery() {
	HandleDatabaseQuery(nil)
}
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	results, err := idx.SemanticSearch("database handler", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	for _, r := range results {
		// Every result must have PathFlags computed (possibly empty).
		// nil vs empty both OK — what we assert is that the classification ran.
		assert.Equal(t, PathFlagStrings(ClassifyPath(r.Symbol.File)), r.PathFlags,
			"PathFlags should match ClassifyPath output for %s", r.Symbol.Name)

		// EdgeCount should be a sensible non-negative integer.
		assert.GreaterOrEqual(t, r.EdgeCount, 0)

		// Test-file results should have the WarnDemotedTest warning.
		if len(r.PathFlags) > 0 {
			for _, f := range r.PathFlags {
				if f == "test" {
					assert.Contains(t, r.ConfidenceWarnings, WarnDemotedTest)
				}
			}
		}
	}
}
