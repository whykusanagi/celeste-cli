package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeStubsTool finds structurally incomplete code — functions and methods
// with zero outgoing call edges in the code graph.
type CodeStubsTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeStubsTool creates a CodeStubsTool backed by the given indexer.
func NewCodeStubsTool(indexer *codegraph.Indexer) *CodeStubsTool {
	return &CodeStubsTool{
		BaseTool: BaseTool{
			ToolName: "code_stubs",
			ToolDescription: "Find structurally incomplete code — functions and methods with zero outgoing call edges, " +
				"indicating stubs, placeholders, or dead code. More powerful than grep for TODO because it detects " +
				"structural isolation in the code graph.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"include_tests": {
						"type": "boolean",
						"description": "Include test files in results (default false)"
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum results to return (default 20)"
					},
					"min_incoming": {
						"type": "integer",
						"description": "Minimum incoming edges to filter by. 0 = completely isolated (default 0)"
					}
				}
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
		indexer: indexer,
	}
}

// isLeafFunction returns true if the function name matches a pattern that is
// expected to have zero outgoing call edges (interface implementations,
// constructors, accessors, etc.).
func isLeafFunction(name string) bool {
	// Exact-match leaf names (interface implementations and trivial methods).
	leafNames := map[string]bool{
		"Close": true, "Init": true, "String": true, "Error": true,
		"Len": true, "Name": true, "Description": true, "Parameters": true,
		"IsReadOnly": true, "ValidateInput": true, "InterruptBehavior": true,
		"Less": true, "Swap": true, "MarshalJSON": true, "UnmarshalJSON": true,
		"Reset": true, "ProtoMessage": true,
	}
	if leafNames[name] {
		return true
	}

	// Prefix-match patterns for constructors, accessors, and test functions.
	prefixes := []string{"New", "Get", "Set", "Is", "Has", "Test", "Benchmark"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return true
		}
	}

	return false
}

// isTestFile returns true if the file path looks like a test file.
func isTestFile(file string) bool {
	return strings.HasSuffix(file, "_test.go") ||
		strings.HasSuffix(file, "_test.py") ||
		strings.HasSuffix(file, ".test.ts") ||
		strings.HasSuffix(file, ".test.js") ||
		strings.HasSuffix(file, ".spec.ts") ||
		strings.HasSuffix(file, ".spec.js") ||
		strings.Contains(file, "/test/") ||
		strings.Contains(file, "/tests/")
}

func (t *CodeStubsTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	includeTests := getBoolArg(input, "include_tests", false)
	maxResults := getIntArg(input, "max_results", 20)
	minIncoming := getIntArg(input, "min_incoming", 0)

	store := t.indexer.Store()
	stubs, err := store.FindStubs(includeTests)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("query error: %s", err)}, nil
	}

	// Filter results.
	var filtered []codegraph.StubResult
	for _, s := range stubs {
		// Skip test files unless requested.
		if !includeTests && isTestFile(s.File) {
			continue
		}
		// Skip known leaf functions.
		if isLeafFunction(s.Name) {
			continue
		}
		// Apply minimum incoming edges filter.
		if s.InEdges < minIncoming {
			continue
		}
		filtered = append(filtered, s)
		if len(filtered) >= maxResults {
			break
		}
	}

	if len(filtered) == 0 {
		return tools.ToolResult{Content: "No stub functions found. All functions/methods have outgoing call edges."}, nil
	}

	// Build structured JSON output.
	type stubEntry struct {
		File          string `json:"file"`
		Line          int    `json:"line"`
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		OutgoingEdges int    `json:"outgoing_edges"`
		IncomingEdges int    `json:"incoming_edges"`
	}

	entries := make([]stubEntry, len(filtered))
	for i, s := range filtered {
		entries[i] = stubEntry{
			File:          s.File,
			Line:          s.Line,
			Name:          s.Name,
			Kind:          s.Kind,
			OutgoingEdges: s.OutEdges,
			IncomingEdges: s.InEdges,
		}
	}

	out, _ := json.MarshalIndent(entries, "", "  ")

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d potential stub functions (0 outgoing call edges):\n\n", len(filtered))
	b.Write(out)

	return tools.ToolResult{Content: b.String()}, nil
}
