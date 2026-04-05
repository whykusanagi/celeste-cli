package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeReviewTool performs automated code review using structural analysis of
// the code graph. It detects issues that grep alone can't find by combining
// graph connectivity (edges, call chains) with body content analysis in a
// single pass over all indexed functions.
type CodeReviewTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeReviewTool creates the unified graph-based code review tool.
func NewCodeReviewTool(indexer *codegraph.Indexer) *CodeReviewTool {
	return &CodeReviewTool{
		BaseTool: BaseTool{
			ToolName: "code_review",
			ToolDescription: "Automated code review using structural graph analysis.\n\n" +
				"Analyzes every function in the code graph in a single pass, detecting issues " +
				"by combining call graph structure with body content:\n\n" +
				"Categories (use 'kinds' to filter):\n\n" +
				"- LAZY_REDIRECT: Functions whose names imply action (handle, execute, process) " +
				"but have minimal call edges and redirect language in body — they tell the user " +
				"to 'run X' instead of doing the work.\n\n" +
				"- STUB: Functions with zero outgoing call edges that aren't expected leaf patterns " +
				"(constructors, getters, interface impls). Likely unfinished implementations.\n\n" +
				"- PLACEHOLDER: Functions with zero edges, short bodies, and placeholder language " +
				"like 'not implemented'.\n\n" +
				"- TODO_FIXME: Unfinished work markers, scored by impact — a TODO in a function " +
				"called by 10 others is more critical than one in dead code.\n\n" +
				"- EMPTY_HANDLER: Error swallowing patterns ('_ = err') in functions that call " +
				"error-returning functions but suppress the errors.\n\n" +
				"- HARDCODED: Hardcoded localhost URLs, IP addresses, or credential values.\n\n" +
				"Each finding includes a score (higher = more critical), reason, and graph context " +
				"(incoming/outgoing edges).\n\n" +
				"IMPORTANT — VERIFY BEFORE REPORTING. The graph has known blind spots. For each finding:\n\n" +
				"1. STUB/PLACEHOLDER: Use search to grep for the function name across ALL files. " +
				"If callers exist in other packages, it's a FALSE POSITIVE (graph missed cross-package call). " +
				"Check if the file has build tags (e.g., _windows.go, _linux.go) — these are platform-specific, not dead.\n\n" +
				"2. LAZY_REDIRECT: Read the function body. If it calls other functions AND has redirect text " +
				"in a help/error string, it's a FALSE POSITIVE — the function does real work.\n\n" +
				"3. DEAD CODE claims: Use search to check if the function is called via callback injection " +
				"(e.g., assigned to a struct field, passed as argument, registered in init()). " +
				"Go init() functions have NO callers by design — they run automatically.\n\n" +
				"4. Check list_files for related files (e.g., _test.go, _windows.go pairs) before claiming dead code.\n\n" +
				"Classify each verified finding as:\n" +
				"- REAL ISSUE: Confirmed by search — no callers, empty body, genuinely broken\n" +
				"- FALSE POSITIVE: Graph missed callers, build tags, callbacks, or init() pattern\n" +
				"- ACCEPTED: Known technical debt (e.g., localhost on single-machine setup)",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"kinds": {
						"type": "string",
						"description": "Comma-separated categories: ALL, LAZY_REDIRECT, STUB, PLACEHOLDER, TODO_FIXME, EMPTY_HANDLER, HARDCODED (default: ALL)"
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum results to return (default 30)"
					},
					"include_tests": {
						"type": "boolean",
						"description": "Include test files in results (default false)"
					}
				}
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
		indexer: indexer,
	}
}

func (t *CodeReviewTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	maxResults := getIntArg(input, "max_results", 30)
	includeTests := getBoolArg(input, "include_tests", false)

	// Parse requested kinds
	kindsStr := "ALL"
	if k, ok := input["kinds"].(string); ok && k != "" {
		kindsStr = strings.ToUpper(k)
	}

	var kinds []codegraph.CodeSmellKind
	if kindsStr != "ALL" {
		for _, k := range strings.Split(kindsStr, ",") {
			kinds = append(kinds, codegraph.CodeSmellKind(strings.TrimSpace(k)))
		}
	}

	results, err := t.indexer.FindCodeSmells(kinds, maxResults, includeTests)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("analysis error: %s", err)}, nil
	}

	if len(results) == 0 {
		return tools.ToolResult{Content: "No issues detected. Codebase looks clean."}, nil
	}

	// Group by kind for readability
	grouped := make(map[codegraph.CodeSmellKind][]codegraph.CodeSmell)
	for _, r := range results {
		grouped[r.Kind] = append(grouped[r.Kind], r)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d issues across %d categories:\n\n", len(results), len(grouped))

	kindOrder := []codegraph.CodeSmellKind{
		codegraph.SmellLazyRedirect,
		codegraph.SmellStub,
		codegraph.SmellPlaceholder,
		codegraph.SmellEmptyHandler,
		codegraph.SmellHardcoded,
		codegraph.SmellTodoFixme,
	}

	for _, kind := range kindOrder {
		items, ok := grouped[kind]
		if !ok {
			continue
		}

		fmt.Fprintf(&b, "## %s (%d)\n\n", kind, len(items))
		out, _ := json.MarshalIndent(items, "", "  ")
		b.Write(out)
		b.WriteString("\n\n")
	}

	b.WriteString("Verify each finding by reading the source before classifying.")

	return tools.ToolResult{Content: b.String()}, nil
}
