package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeSymbolsTool lists symbols in a file or package without reading full source.
// Solves the "main.go is 3000 lines" problem by showing what's in a file before
// the model decides what to read.
type CodeSymbolsTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeSymbolsTool creates a CodeSymbolsTool backed by the given indexer.
func NewCodeSymbolsTool(indexer *codegraph.Indexer) *CodeSymbolsTool {
	return &CodeSymbolsTool{
		BaseTool: BaseTool{
			ToolName: "code_symbols",
			ToolDescription: "List all symbols (functions, types, interfaces, etc.) in a file or package. " +
				"Use this before read_file to understand what's in a large file without reading the entire source. " +
				"Provide either a file path or a package name.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file": {
						"type": "string",
						"description": "Relative file path to list symbols for."
					},
					"package": {
						"type": "string",
						"description": "Package name to list symbols for."
					}
				}
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
		indexer: indexer,
	}
}

func (t *CodeSymbolsTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	file := getStringArg(input, "file", "")
	pkg := getStringArg(input, "package", "")

	if file == "" && pkg == "" {
		return tools.ToolResult{Error: true, Content: "provide either 'file' or 'package'"}, nil
	}

	store := t.indexer.Store()
	var syms []codegraph.Symbol
	var err error
	var label string

	if file != "" {
		syms, err = store.GetSymbolsByFile(file)
		label = file
	} else {
		syms, err = store.GetSymbolsByPackage(pkg)
		label = "package " + pkg
	}

	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("query error: %s", err)}, nil
	}

	if len(syms) == 0 {
		return tools.ToolResult{Content: fmt.Sprintf("No symbols found in %s.", label)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Symbols in %s (%d total):\n\n", label, len(syms))

	// Group by kind for readability
	grouped := make(map[codegraph.SymbolKind][]codegraph.Symbol)
	kindOrder := []codegraph.SymbolKind{
		codegraph.SymbolInterface, codegraph.SymbolStruct, codegraph.SymbolType,
		codegraph.SymbolFunction, codegraph.SymbolMethod,
		codegraph.SymbolConst, codegraph.SymbolVar,
	}

	for _, s := range syms {
		grouped[s.Kind] = append(grouped[s.Kind], s)
	}

	for _, kind := range kindOrder {
		items := grouped[kind]
		if len(items) == 0 {
			continue
		}

		kindLabel := strings.ToUpper(string(kind)[:1]) + string(kind)[1:]
		fmt.Fprintf(&b, "### %ss (%d)\n", kindLabel, len(items))
		for _, s := range items {
			if s.Signature != "" {
				fmt.Fprintf(&b, "  L%-4d %s\n", s.Line, s.Signature)
			} else {
				fmt.Fprintf(&b, "  L%-4d %s %s\n", s.Line, s.Kind, s.Name)
			}
		}
		b.WriteString("\n")
	}

	// Show any remaining kinds not in kindOrder
	for kind, items := range grouped {
		found := false
		for _, k := range kindOrder {
			if k == kind {
				found = true
				break
			}
		}
		if !found && len(items) > 0 {
			fmt.Fprintf(&b, "### %ss (%d)\n", string(kind), len(items))
			for _, s := range items {
				fmt.Fprintf(&b, "  L%-4d %s\n", s.Line, s.Name)
			}
			b.WriteString("\n")
		}
	}

	return tools.ToolResult{Content: b.String()}, nil
}
