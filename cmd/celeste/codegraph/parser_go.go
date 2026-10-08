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
	// SourceScope is the class chain the source is declared in
	// (Symbol.Scope), so same-named methods of different classes keep
	// their own edges. Empty for a function outside any class and for Go.
	SourceScope string
	TargetName  string
	Kind        EdgeKind
	// SelfCall marks a call on the caller's own object written without
	// the receiver in TargetName ($this->m() in PHP, an unqualified call
	// in a Java, Ruby or C++ method): it resolves to the method of
	// SourceScope first, like a self.m / this.m target.
	SelfCall bool
	// SourceFile is the workspace-relative file the edge was parsed from.
	// Parsers leave it empty; the indexer sets it so both ends resolve
	// against that file's symbols before any same-named symbol elsewhere.
	SourceFile string
}

// GoParser extracts symbols and edges from Go source files using go/ast.
type GoParser struct{}

// NewGoParser creates a new Go AST parser.
func NewGoParser() *GoParser {
	return &GoParser{}
}

// ParseFile parses a single Go source file and extracts symbols and edges.
func (p *GoParser) ParseFile(path string) (*ParseResult, error) {
	return p.ParseSource(path, nil)
}

// ParseSource is ParseFile of content already read; path only names it.
// A nil src reads path.
func (p *GoParser) ParseSource(path string, src []byte) (*ParseResult, error) {
	fset := token.NewFileSet()
	var source any
	if src != nil {
		source = src
	}
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	result := &ParseResult{Symbols: p.fileSymbols(file, path, fset)}
	for _, decl := range file.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Body != nil {
			result.Edges = append(result.Edges, p.extractCallEdges(d, fset)...)
		}
	}
	return result, nil
}

// fileSymbols extracts the declared symbols (imports, functions, methods,
// types, interface methods, consts, vars) of one parsed file. Shared by the
// heuristic ParseFile and the type-checked pass in gotypes.go.
func (p *GoParser) fileSymbols(file *ast.File, path string, fset *token.FileSet) []Symbol {
	var syms []Symbol
	pkgName := file.Name.Name

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		syms = append(syms, Symbol{
			Name:    importPath,
			Kind:    SymbolImport,
			Package: pkgName,
			File:    path,
			Line:    fset.Position(imp.Pos()).Line,
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			syms = append(syms, p.extractFunction(d, pkgName, path, fset))
		case *ast.GenDecl:
			syms = append(syms, p.extractGenDecl(d, pkgName, path, fset)...)
		}
	}
	return syms
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

// formatInterfaceMethod renders an interface method like a method
// signature on the interface: "func (Writer) Write([]byte) (int, error)".
func formatInterfaceMethod(iface, name string, ft *ast.FuncType) string {
	var b strings.Builder
	b.WriteString("func (" + iface + ") " + name + "(")
	if ft.Params != nil {
		b.WriteString(formatFieldList(ft.Params))
	}
	b.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		b.WriteString(" ")
		if len(ft.Results.List) > 1 || len(ft.Results.List[0].Names) > 0 {
			b.WriteString("(" + formatFieldList(ft.Results) + ")")
		} else {
			b.WriteString(formatFieldList(ft.Results))
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
			if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
				for _, m := range it.Methods.List {
					ft, ok := m.Type.(*ast.FuncType)
					if !ok {
						continue // embedded interface or type-set term
					}
					for _, name := range m.Names {
						syms = append(syms, Symbol{
							Name:      name.Name,
							Kind:      SymbolInterfaceMethod,
							Package:   pkg,
							File:      file,
							Line:      fset.Position(name.Pos()).Line,
							Signature: formatInterfaceMethod(s.Name.Name, name.Name, ft),
						})
					}
				}
			}

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
