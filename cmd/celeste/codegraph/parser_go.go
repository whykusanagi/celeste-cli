package codegraph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ParseResult holds the symbols and edges extracted from a single file.
type ParseResult struct {
	Symbols []Symbol
	Edges   []RawEdge
	Source  []byte // raw file content for shingle generation
}

// RawEdge is an unresolved edge that uses symbol names instead of IDs.
// Resolved to Edge (with IDs) when inserted into the store.
type RawEdge struct {
	SourceName string
	TargetName string
	Kind       EdgeKind
}

// GoParser extracts symbols and edges from Go source files using go/ast.
type GoParser struct{}

// NewGoParser creates a new Go AST parser.
func NewGoParser() *GoParser {
	return &GoParser{}
}

// ParseFile parses a single Go source file and extracts symbols and edges.
func (p *GoParser) ParseFile(path string) (*ParseResult, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	result := &ParseResult{}
	pkgName := file.Name.Name

	// Extract import edges
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		result.Symbols = append(result.Symbols, Symbol{
			Name:    importPath,
			Kind:    SymbolImport,
			Package: pkgName,
			File:    path,
			Line:    fset.Position(imp.Pos()).Line,
		})
	}

	// Walk the AST for declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := p.extractFunction(d, pkgName, path, fset)
			result.Symbols = append(result.Symbols, sym)

			// Extract call edges from function body
			if d.Body != nil {
				edges := p.extractCallEdges(d, fset)
				result.Edges = append(result.Edges, edges...)
			}

		case *ast.GenDecl:
			syms := p.extractGenDecl(d, pkgName, path, fset)
			result.Symbols = append(result.Symbols, syms...)
		}
	}

	return result, nil
}

// extractFunction extracts a function or method symbol.
func (p *GoParser) extractFunction(fn *ast.FuncDecl, pkg, file string, fset *token.FileSet) Symbol {
	kind := SymbolFunction
	if fn.Recv != nil {
		kind = SymbolMethod
	}

	sig := p.formatFuncSignature(fn)

	return Symbol{
		Name:      fn.Name.Name,
		Kind:      kind,
		Package:   pkg,
		File:      file,
		Line:      fset.Position(fn.Pos()).Line,
		Signature: sig,
	}
}

// formatFuncSignature builds a human-readable function signature.
func (p *GoParser) formatFuncSignature(fn *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")

	// Receiver
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(typeString(fn.Recv.List[0].Type))
		b.WriteString(") ")
	}

	b.WriteString(fn.Name.Name)
	b.WriteString("(")

	// Parameters
	if fn.Type.Params != nil {
		params := formatFieldList(fn.Type.Params)
		b.WriteString(params)
	}
	b.WriteString(")")

	// Return types
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		b.WriteString(" ")
		results := formatFieldList(fn.Type.Results)
		if len(fn.Type.Results.List) > 1 {
			b.WriteString("(")
			b.WriteString(results)
			b.WriteString(")")
		} else {
			b.WriteString(results)
		}
	}

	return b.String()
}

// extractGenDecl extracts symbols from general declarations (type, const, var).
func (p *GoParser) extractGenDecl(decl *ast.GenDecl, pkg, file string, fset *token.FileSet) []Symbol {
	var syms []Symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := SymbolType
			switch s.Type.(type) {
			case *ast.InterfaceType:
				kind = SymbolInterface
			case *ast.StructType:
				kind = SymbolStruct
			}
			syms = append(syms, Symbol{
				Name:    s.Name.Name,
				Kind:    kind,
				Package: pkg,
				File:    file,
				Line:    fset.Position(s.Pos()).Line,
			})

		case *ast.ValueSpec:
			kind := SymbolVar
			if decl.Tok == token.CONST {
				kind = SymbolConst
			}
			for _, name := range s.Names {
				if name.Name == "_" {
					continue
				}
				syms = append(syms, Symbol{
					Name:    name.Name,
					Kind:    kind,
					Package: pkg,
					File:    file,
					Line:    fset.Position(name.Pos()).Line,
				})
			}
		}
	}

	return syms
}

// extractCallEdges detects function calls inside a function body.
func (p *GoParser) extractCallEdges(fn *ast.FuncDecl, fset *token.FileSet) []RawEdge {
	var edges []RawEdge
	callerName := fn.Name.Name

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			// Direct call: helper()
			edges = append(edges, RawEdge{
				SourceName: callerName,
				TargetName: fun.Name,
				Kind:       EdgeCalls,
			})
		case *ast.SelectorExpr:
			// Qualified call: pkg.Func() or obj.Method()
			if ident, ok := fun.X.(*ast.Ident); ok {
				edges = append(edges, RawEdge{
					SourceName: callerName,
					TargetName: ident.Name + "." + fun.Sel.Name,
					Kind:       EdgeCalls,
				})
			}
		}
		return true
	})

	return edges
}

// typeString converts an AST type expression to a string representation.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + typeString(t.Value)
	default:
		return "unknown"
	}
}

// formatFieldList formats a parameter or result list for signatures.
func formatFieldList(fl *ast.FieldList) string {
	var parts []string
	for _, field := range fl.List {
		typeName := typeString(field.Type)
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeName)
			}
		} else {
			parts = append(parts, typeName)
		}
	}
	return strings.Join(parts, ", ")
}
