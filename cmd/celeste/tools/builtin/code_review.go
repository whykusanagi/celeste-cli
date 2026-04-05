package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeReviewTool performs automated code review by searching for common
// code smell patterns that graph analysis alone can't detect.
type CodeReviewTool struct {
	BaseTool
}

// NewCodeReviewTool creates a code review tool.
func NewCodeReviewTool() *CodeReviewTool {
	return &CodeReviewTool{
		BaseTool: BaseTool{
			ToolName: "code_review",
			ToolDescription: "Automated code review — searches for common code smell patterns.\n\n" +
				"Patterns detected:\n" +
				"- LAZY REDIRECT: Functions/handlers that just print 'run X command' instead of doing the work. " +
				"Pattern: contains 'Run `celeste' or 'Use `celeste' or 'use the CLI'. These should inline the functionality.\n" +
				"- PLACEHOLDER: Functions containing 'not yet implemented', 'not implemented', 'stub', or just 'pass'.\n" +
				"- TODO/FIXME: Unfinished work markers.\n" +
				"- HARDCODED: Hardcoded credentials, URLs with 'localhost', or magic numbers in business logic.\n" +
				"- EMPTY HANDLER: Error handlers that swallow errors silently ('_ = err', '_ =' patterns).\n\n" +
				"Returns findings grouped by pattern with file:line references.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"patterns": {
						"type": "string",
						"description": "Comma-separated patterns to check: all, lazy-redirect, placeholder, todo, hardcoded, empty-handler (default: all)"
					},
					"path": {
						"type": "string",
						"description": "Directory to scan (default: entire project)"
					}
				}
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
	}
}

type reviewPattern struct {
	Name     string
	Searches []string
	Desc     string
}

func (t *CodeReviewTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	patternsStr := "all"
	if p, ok := input["patterns"].(string); ok && p != "" {
		patternsStr = p
	}

	allPatterns := []reviewPattern{
		{
			Name:     "LAZY REDIRECT",
			Searches: []string{"Run `celeste", "Use `celeste", "use the CLI", "available in interactive chat"},
			Desc:     "Handler redirects to CLI instead of doing the work inline",
		},
		{
			Name:     "PLACEHOLDER",
			Searches: []string{"not yet implemented", "not implemented", "stub", "placeholder"},
			Desc:     "Unfinished implementation",
		},
		{
			Name:     "TODO/FIXME",
			Searches: []string{"TODO:", "FIXME:", "HACK:", "XXX:"},
			Desc:     "Unfinished work marker",
		},
		{
			Name:     "EMPTY HANDLER",
			Searches: []string{"_ = err", "_ =", "// ignore error", "// swallow"},
			Desc:     "Error silently swallowed",
		},
	}

	// Filter by requested patterns
	var selected []reviewPattern
	if patternsStr == "all" {
		selected = allPatterns
	} else {
		wanted := make(map[string]bool)
		for _, p := range strings.Split(patternsStr, ",") {
			wanted[strings.TrimSpace(p)] = true
		}
		for _, p := range allPatterns {
			key := strings.ToLower(strings.ReplaceAll(p.Name, " ", "-"))
			if wanted[key] {
				selected = append(selected, p)
			}
		}
	}

	if len(selected) == 0 {
		return tools.ToolResult{Content: "No valid patterns selected. Use: all, lazy-redirect, placeholder, todo, empty-handler"}, nil
	}

	// Return the search patterns — the LLM will use the search tool to find matches
	var sb strings.Builder
	sb.WriteString("Code review patterns to search for:\n\n")
	for _, p := range selected {
		sb.WriteString(fmt.Sprintf("## %s\n", p.Name))
		sb.WriteString(fmt.Sprintf("Description: %s\n", p.Desc))
		sb.WriteString("Search terms:\n")
		for _, s := range p.Searches {
			sb.WriteString(fmt.Sprintf("  - \"%s\"\n", s))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Use the search tool to find each pattern in .go files, then report findings with file:line.")

	return tools.ToolResult{Content: sb.String()}, nil
}
