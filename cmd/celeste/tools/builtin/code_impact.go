package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeImpactTool analyzes the blast radius of code changes.
type CodeImpactTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

func NewCodeImpactTool(indexer *codegraph.Indexer) *CodeImpactTool {
	return &CodeImpactTool{
		BaseTool: BaseTool{
			ToolName: "code_impact",
			ToolDescription: "Analyze the blast radius of code changes. Maps git diff to directly changed symbols, " +
				"their transitive callers, risk scores, and test coverage gaps. Use before code review to " +
				"understand what a change affects.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"base": {
						"type": "string",
						"description": "Git ref to diff against. Default: HEAD~1. Examples: HEAD~3, main, abc123"
					}
				}
			}`),
			ReadOnly: true,
		},
		indexer: indexer,
	}
}

func (t *CodeImpactTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	base, _ := input["base"].(string)
	if base == "" {
		base = "HEAD~1"
	}

	result, err := t.indexer.AnalyzeChanges(base)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Impact analysis failed: %v", err), Error: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Change impact analysis (diff against %s):\n\n", base))
	sb.WriteString(fmt.Sprintf("Files changed: %d\n", result.Summary.FilesChanged))
	sb.WriteString(fmt.Sprintf("Symbols directly changed: %d\n", result.Summary.SymbolsChanged))
	sb.WriteString(fmt.Sprintf("Callers affected (blast radius): %d\n", result.Summary.CallersAffected))
	sb.WriteString(fmt.Sprintf("Test coverage gaps: %d\n", result.Summary.TestGaps))
	sb.WriteString(fmt.Sprintf("Max risk score: %.2f\n\n", result.Summary.MaxRiskScore))

	if len(result.DirectlyChanged) > 0 {
		sb.WriteString("Directly changed symbols (by risk):\n")
		for i, sym := range result.DirectlyChanged {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(result.DirectlyChanged)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("  [%.2f] %s %s (%s:%d)\n",
				sym.RiskScore, sym.Kind, sym.Name, sym.File, sym.Line))
		}
		sb.WriteString("\n")
	}

	if len(result.AffectedCallers) > 0 {
		sb.WriteString("Affected callers (transitive blast radius):\n")
		for i, sym := range result.AffectedCallers {
			if i >= 15 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(result.AffectedCallers)-15))
				break
			}
			sb.WriteString(fmt.Sprintf("  [%.2f] %s %s (%s:%d)\n",
				sym.RiskScore, sym.Kind, sym.Name, sym.File, sym.Line))
		}
		sb.WriteString("\n")
	}

	if len(result.UncoveredByTests) > 0 {
		sb.WriteString("Test coverage gaps (changed symbols with no test callers):\n")
		for i, name := range result.UncoveredByTests {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(result.UncoveredByTests)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", name))
		}
	}

	return tools.ToolResult{
		Content: sb.String(),
		Metadata: map[string]any{
			"files_changed":    result.Summary.FilesChanged,
			"symbols_changed":  result.Summary.SymbolsChanged,
			"callers_affected": result.Summary.CallersAffected,
			"test_gaps":        result.Summary.TestGaps,
			"max_risk":         result.Summary.MaxRiskScore,
		},
	}, nil
}

// CodeSnapshotTool takes and manages graph snapshots.
type CodeSnapshotTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

func NewCodeSnapshotTool(indexer *codegraph.Indexer) *CodeSnapshotTool {
	return &CodeSnapshotTool{
		BaseTool: BaseTool{
			ToolName: "code_snapshot",
			ToolDescription: "Take a snapshot of the code graph state for later diffing. " +
				"Use action 'save' to capture current state, 'diff' to compare against the last snapshot.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["save", "diff"],
						"description": "Action to perform. 'save' captures current state. 'diff' compares against last snapshot."
					}
				},
				"required": ["action"]
			}`),
			ReadOnly: false,
		},
		indexer: indexer,
	}
}

func (t *CodeSnapshotTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	action, _ := input["action"].(string)

	switch action {
	case "save":
		snap, err := t.indexer.TakeSnapshot()
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Snapshot failed: %v", err), Error: true}, nil
		}
		if err := t.indexer.SaveSnapshot(snap); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Save failed: %v", err), Error: true}, nil
		}
		return tools.ToolResult{
			Content: fmt.Sprintf("Snapshot saved at commit %s: %d symbols, %d edges",
				snap.CommitSHA, snap.SymbolCount, snap.EdgeCount),
			Metadata: map[string]any{
				"commit":  snap.CommitSHA,
				"symbols": snap.SymbolCount,
				"edges":   snap.EdgeCount,
			},
		}, nil

	case "diff":
		current, err := t.indexer.TakeSnapshot()
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Snapshot failed: %v", err), Error: true}, nil
		}
		previous, err := t.indexer.LatestSnapshot()
		if err != nil || previous == nil {
			return tools.ToolResult{
				Content: "No previous snapshot to diff against. Use action 'save' first.",
				Error:   true,
			}, nil
		}
		diff := codegraph.DiffSnapshots(previous, current)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Graph diff: %s → %s\n", diff.BeforeSHA, diff.AfterSHA))
		sb.WriteString(fmt.Sprintf("Symbols: +%d / -%d\n", diff.Summary.SymbolsAdded, diff.Summary.SymbolsRemoved))
		sb.WriteString(fmt.Sprintf("Edges: +%d / -%d\n", diff.Summary.EdgesAdded, diff.Summary.EdgesRemoved))
		if len(diff.AddedSymbols) > 0 {
			limit := len(diff.AddedSymbols)
			if limit > 20 {
				limit = 20
			}
			sb.WriteString(fmt.Sprintf("\nAdded symbols (%d total):\n", len(diff.AddedSymbols)))
			for _, s := range diff.AddedSymbols[:limit] {
				sb.WriteString(fmt.Sprintf("  + %s\n", s))
			}
		}
		if len(diff.RemovedSymbols) > 0 {
			limit := len(diff.RemovedSymbols)
			if limit > 20 {
				limit = 20
			}
			sb.WriteString(fmt.Sprintf("\nRemoved symbols (%d total):\n", len(diff.RemovedSymbols)))
			for _, s := range diff.RemovedSymbols[:limit] {
				sb.WriteString(fmt.Sprintf("  - %s\n", s))
			}
		}
		return tools.ToolResult{
			Content: sb.String(),
			Metadata: map[string]any{
				"symbols_added":   diff.Summary.SymbolsAdded,
				"symbols_removed": diff.Summary.SymbolsRemoved,
				"edges_added":     diff.Summary.EdgesAdded,
				"edges_removed":   diff.Summary.EdgesRemoved,
			},
		}, nil

	default:
		return tools.ToolResult{Content: "Invalid action. Use 'save' or 'diff'.", Error: true}, nil
	}
}
