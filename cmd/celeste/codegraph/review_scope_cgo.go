//go:build cgo

package codegraph

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// treeSitterSpans parses src with the tree-sitter grammar for relPath's
// language and returns the span of every function the indexer's walker
// records as a symbol.
func (r *reviewer) treeSitterSpans(relPath string, src []byte) ([]funcSpan, bool) {
	lang := SupportedLanguage(strings.ToLower(filepath.Ext(relPath)))
	if lang == "" || lang == "go" || !multiLangGrammars[lang] {
		return nil, false
	}
	spec, ok := langSpecs[lang]
	if !ok || spec.FunctionTypes == nil {
		return nil, false
	}
	if r.ts == nil {
		r.ts = NewMultiLangParser()
	}
	grammar, ok := r.ts.langs[lang]
	if !ok {
		return nil, false
	}
	if err := r.ts.parser.SetLanguage(grammar); err != nil {
		return nil, false
	}
	tree := r.ts.parser.Parse(src, nil)
	if tree == nil {
		return nil, false
	}
	defer tree.Close()

	w := &multiWalker{
		src:      src,
		lang:     lang,
		spec:     &spec,
		classSet: nodeTypeSet(spec.ClassTypes),
		funcSet:  nodeTypeSet(spec.FunctionTypes),
	}
	var spans []funcSpan
	w.collectSpans(tree.RootNode(), "", false, &spans)
	return spans, true
}

// collectSpans mirrors walk: it visits the same class, function and
// JS/TS variable-declarator nodes, and records each named function's span.
func (w *multiWalker) collectSpans(node *tree_sitter.Node, class string, classBases bool, out *[]funcSpan) {
	if node == nil {
		return
	}
	kind := node.Kind()
	if w.classSet[kind] {
		name := w.extractName(node)
		bases := classHasBases(node)
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.collectSpans(node.NamedChild(i), name, bases, out)
		}
		return
	}
	if w.funcSet[kind] {
		if name := w.extractName(node); name != "" {
			*out = append(*out, w.spanOf(node, node, name, class, classBases))
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.collectSpans(node.NamedChild(i), "", false, out)
		}
		return
	}
	if (kind == "lexical_declaration" || kind == "variable_declaration") &&
		(w.lang == "javascript" || w.lang == "typescript" || w.lang == "tsx") {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			decl := node.NamedChild(i)
			if decl == nil || decl.Kind() != "variable_declarator" {
				continue
			}
			nameNode, value := decl.ChildByFieldName("name"), decl.ChildByFieldName("value")
			if nameNode != nil && value != nil {
				switch value.Kind() {
				case "arrow_function", "function_expression", "function":
					*out = append(*out, w.spanOf(decl, value, w.nodeText(nameNode), "", false))
					for j := uint(0); j < value.NamedChildCount(); j++ {
						w.collectSpans(value.NamedChild(j), "", false, out)
					}
					continue
				}
			}
			w.collectSpans(decl, class, classBases, out)
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		w.collectSpans(node.NamedChild(i), class, classBases, out)
	}
}

// spanOf builds the span of a function: outer is the node whose extent and
// start line the index records (the variable declarator of `const f = () =>
// …`, else the function itself), fn the function node.
func (w *multiWalker) spanOf(outer, fn *tree_sitter.Node, name, class string, classBases bool) funcSpan {
	s := funcSpan{
		Name:          name,
		Start:         int(outer.StartPosition().Row) + 1,
		End:           int(outer.EndPosition().Row) + 1,
		Class:         class,
		ClassHasBases: classBases,
		Exact:         true,
	}
	// tree-sitter ends a node at the position after its last byte; a node
	// that ends at column 0 ends on the line before.
	if outer.EndPosition().Column == 0 && s.End > s.Start {
		s.End--
	}
	body := fn.ChildByFieldName("body")
	switch {
	case body != nil:
		s.HasBody = true
		for i := uint(0); i < body.NamedChildCount(); i++ {
			child := body.NamedChild(i)
			if child != nil && !strings.Contains(child.Kind(), "comment") {
				s.Stmts = append(s.Stmts, w.nodeText(child))
			}
		}
	case w.lang == "ruby":
		// `def x; end` has no body node: an empty body, not a declaration.
		s.HasBody = true
	}
	w.collectComments(fn, &s.Comments)

	kind := fn.Kind()
	switch {
	case kind == "constructor_declaration":
		s.Constructor = true
	case w.lang == "ruby" && name == "initialize",
		w.lang == "php" && strings.EqualFold(name, "__construct"),
		(w.lang == "typescript" || w.lang == "tsx" || w.lang == "javascript") && name == "constructor":
		s.Constructor = true
	case w.lang == "cpp" || w.lang == "c":
		s.Constructor = isCppConstructor(name, class)
	}
	if p := outer.Parent(); p != nil && p.Kind() == "export_statement" {
		s.Exported = true
	} else if p != nil && p.Kind() == "lexical_declaration" {
		if gp := p.Parent(); gp != nil && gp.Kind() == "export_statement" {
			s.Exported = true
		}
	}
	for i := uint(0); i < fn.NamedChildCount(); i++ {
		child := fn.NamedChild(i)
		if child == nil || child.Kind() != "modifiers" {
			continue
		}
		for j := uint(0); j < child.NamedChildCount(); j++ {
			ann := child.NamedChild(j)
			if ann == nil || (ann.Kind() != "marker_annotation" && ann.Kind() != "annotation") {
				continue
			}
			if n := ann.ChildByFieldName("name"); n != nil {
				s.Annotations = append(s.Annotations, w.nodeText(n))
			}
		}
	}
	return s
}

// collectComments appends the text of every comment inside node, skipping
// the comments of nested functions.
func (w *multiWalker) collectComments(node *tree_sitter.Node, out *[]string) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || w.funcSet[child.Kind()] {
			continue
		}
		if strings.Contains(child.Kind(), "comment") {
			*out = append(*out, w.nodeText(child))
			continue
		}
		w.collectComments(child, out)
	}
}

// classHasBases reports whether a class declaration extends or implements
// another type, in any of the grammars' spellings.
func classHasBases(node *tree_sitter.Node) bool {
	for _, field := range []string{"superclasses", "superclass", "interfaces"} {
		if node.ChildByFieldName(field) != nil {
			return true
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		switch node.NamedChild(i).Kind() {
		case "superclass", "super_interfaces", "class_heritage", "base_clause",
			"class_interface_clause", "base_class_clause":
			return true
		}
	}
	return false
}

// isCppConstructor reports whether a C++ function name is a constructor or
// destructor: the class's own name inside its body, or `A::A` / `A::~A`.
func isCppConstructor(name, class string) bool {
	if class != "" && (name == class || name == "~"+class) {
		return true
	}
	if i := strings.LastIndex(name, "::"); i > 0 {
		scope, member := name[:i], strings.TrimPrefix(name[i+2:], "~")
		if j := strings.LastIndex(scope, "::"); j >= 0 {
			scope = scope[j+2:]
		}
		return scope == member
	}
	return false
}
