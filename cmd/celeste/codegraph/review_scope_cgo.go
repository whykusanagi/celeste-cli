//go:build cgo

package codegraph

import (
	"path"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// treeSitterSpans parses src with the tree-sitter grammar of lang (the
// grammar name SupportedLanguage gives for the file's extension) and
// returns the span of every function the indexer's walker records as a
// symbol, and the base types of every class it declares.
func (r *reviewer) treeSitterSpans(lang, relPath string, src []byte) ([]funcSpan, map[string][]string, bool) {
	if lang == "" || lang == "go" || !multiLangGrammars[lang] {
		return nil, nil, false
	}
	spec, ok := langSpecs[lang]
	if !ok || spec.FunctionTypes == nil {
		return nil, nil, false
	}
	if r.ts == nil {
		r.ts = NewMultiLangParser()
	}
	grammar, ok := r.ts.langs[lang]
	if !ok {
		return nil, nil, false
	}
	if err := r.ts.parser.SetLanguage(grammar); err != nil {
		return nil, nil, false
	}
	tree := r.ts.parser.Parse(src, nil)
	if tree == nil {
		return nil, nil, false
	}
	defer tree.Close()

	w := &multiWalker{
		src:      src,
		path:     relPath,
		lang:     lang,
		spec:     &spec,
		classSet: nodeTypeSet(spec.ClassTypes),
		funcSet:  nodeTypeSet(spec.FunctionTypes),
	}
	out := &spanOut{bases: map[string][]string{}}
	w.collectSpans(tree.RootNode(), spanCtx{}, out)
	return out.spans, out.bases, true
}

// spanOut collects what collectSpans finds: function and member
// declaration spans, and the base types of each class by class name.
type spanOut struct {
	spans []funcSpan
	bases map[string][]string
}

// spanCtx is the class a node is declared in.
type spanCtx struct {
	class    string
	bases    bool   // the class extends or implements a type
	public   bool   // Java: the class is public
	exported bool   // JS/TS: the class is exported
	trait    string // Rust: the trait of an `impl Trait for X`
	// hidden: C++, the members here are not public (a class's default
	// access, or after private: or protected:).
	hidden bool
}

// interfaceTypes are the interface containers the walker does not treat as
// classes but whose members are bodiless declarations a class implements.
var interfaceTypes = map[string]bool{"interface_declaration": true}

// memberDeclTypes are bodiless member declarations: TS interface and
// abstract method signatures.
var memberDeclTypes = map[string]bool{"method_signature": true, "abstract_method_signature": true}

// collectSpans mirrors walk: it visits the same class, function and
// JS/TS variable-declarator nodes, and records each named function's span.
// It also records the bodiless member declarations a method can implement
// or define (TS interface and abstract signatures, C++ member declarations,
// Rust trait signatures), which are no symbols but tell code review a
// method implements one, or what a C++ method defined outside its class is.
func (w *multiWalker) collectSpans(node *tree_sitter.Node, ctx spanCtx, out *spanOut) {
	if node == nil {
		return
	}
	kind := node.Kind()
	if w.classSet[kind] || (interfaceTypes[kind] && w.isJSLike()) {
		inner := spanCtx{class: w.extractName(node), bases: classHasBases(node), public: w.hasModifier(node, "public"),
			hidden: w.lang == "cpp" && kind == "class_specifier"}
		if inner.class != "" {
			out.bases[inner.class] = append(out.bases[inner.class], w.classBaseTypes(node)...)
		}
		if p := node.Parent(); p != nil && p.Kind() == "export_statement" {
			inner.exported = true
		}
		if t := node.ChildByFieldName("trait"); t != nil && w.lang == "rust" && kind == "impl_item" {
			inner.trait = w.nodeText(t)
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.collectSpans(node.NamedChild(i), inner, out)
		}
		return
	}
	if w.funcSet[kind] {
		if name := w.extractName(node); name != "" {
			out.spans = append(out.spans, w.spanOf(node, node, name, ctx))
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.collectSpans(node.NamedChild(i), spanCtx{}, out)
		}
		return
	}
	if ctx.class != "" && w.isJSLike() && memberDeclTypes[kind] {
		if name := w.extractName(node); name != "" {
			out.spans = append(out.spans, w.declSpan(node, name, ctx))
		}
		return
	}
	if ctx.class != "" && w.lang == "rust" && kind == "function_signature_item" {
		if name := w.extractName(node); name != "" {
			out.spans = append(out.spans, w.declSpan(node, name, ctx))
		}
		return
	}
	if ctx.class != "" && w.lang == "cpp" && kind == "field_declaration_list" {
		// An access specifier sets the access of the members after it.
		inner := ctx
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child != nil && child.Kind() == "access_specifier" {
				inner.hidden = strings.TrimSpace(w.nodeText(child)) != "public"
				continue
			}
			w.collectSpans(child, inner, out)
		}
		return
	}
	if ctx.class != "" && w.lang == "cpp" && kind == "field_declaration" {
		if d := node.ChildByFieldName("declarator"); d != nil && d.Kind() == "function_declarator" {
			if name := extractCIdentifier(w.nodeText(d)); name != "" {
				s := w.declSpan(node, name, ctx)
				if cppOverrides(node) {
					s.Annotations = append(s.Annotations, "Override")
				}
				s.Exported = w.cppAPI(ctx)
				out.spans = append(out.spans, s)
			}
			return
		}
	}
	if (kind == "lexical_declaration" || kind == "variable_declaration") && w.isJSLike() {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			decl := node.NamedChild(i)
			if decl == nil || decl.Kind() != "variable_declarator" {
				continue
			}
			nameNode, value := decl.ChildByFieldName("name"), decl.ChildByFieldName("value")
			if nameNode != nil && value != nil {
				switch value.Kind() {
				case "arrow_function", "function_expression", "function":
					out.spans = append(out.spans, w.spanOf(decl, value, w.nodeText(nameNode), spanCtx{}))
					for j := uint(0); j < value.NamedChildCount(); j++ {
						w.collectSpans(value.NamedChild(j), spanCtx{}, out)
					}
					continue
				}
			}
			w.collectSpans(decl, ctx, out)
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		w.collectSpans(node.NamedChild(i), ctx, out)
	}
}

func (w *multiWalker) isJSLike() bool {
	return w.lang == "javascript" || w.lang == "typescript" || w.lang == "tsx"
}

// declSpan is the span of a bodiless member declaration.
func (w *multiWalker) declSpan(node *tree_sitter.Node, name string, ctx spanCtx) funcSpan {
	return funcSpan{
		Name:          name,
		Start:         int(node.StartPosition().Row) + 1,
		End:           int(node.EndPosition().Row) + 1,
		Class:         ctx.class,
		ClassHasBases: ctx.bases,
		Exact:         true,
	}
}

// hasModifier reports a Java modifier (public, abstract, …) on a
// declaration.
func (w *multiWalker) hasModifier(node *tree_sitter.Node, mod string) bool {
	if w.lang != "java" {
		return false
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "modifiers" {
			for _, f := range strings.Fields(w.nodeText(child)) {
				if f == mod {
					return true
				}
			}
		}
	}
	return false
}

// blockTypes are the statement-list body nodes; any other body node (an
// arrow function's `=> expr`, a Ruby endless method) is one expression
// statement.
var blockTypes = map[string]bool{
	"statement_block": true, "block": true, "compound_statement": true,
	"body_statement": true, "constructor_body": true, "function_body": true,
}

// spanOf builds the span of a function: outer is the node whose extent and
// start line the index records (the variable declarator of `const f = () =>
// …`, else the function itself), fn the function node.
func (w *multiWalker) spanOf(outer, fn *tree_sitter.Node, name string, ctx spanCtx) funcSpan {
	class := ctx.class
	s := funcSpan{
		Name:          name,
		Start:         int(outer.StartPosition().Row) + 1,
		End:           int(outer.EndPosition().Row) + 1,
		Class:         class,
		ClassHasBases: ctx.bases,
		Exact:         true,
	}
	// tree-sitter ends a node at the position after its last byte; a node
	// that ends at column 0 ends on the line before.
	if outer.EndPosition().Column == 0 && s.End > s.Start {
		s.End--
	}
	body := fn.ChildByFieldName("body")
	switch {
	case body != nil && !blockTypes[body.Kind()]:
		// An expression body: `() => 0` returns its expression.
		s.HasBody = true
		s.Stmts = []string{w.nodeText(body)}
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
	// Java's exported API: public members of a public class.
	if w.lang == "java" && ctx.public && w.hasModifier(fn, "public") {
		s.Exported = true
	}
	// JS/TS: the public methods of an exported class.
	if w.isJSLike() && ctx.exported && kind == "method_definition" && w.publicMethod(fn) {
		s.Exported = true
	}
	// PHP and Ruby: the public methods of a class (every class is visible
	// to the code that loads its file). C++: the public members of a class
	// declared in a header, which other translation units include.
	switch {
	case w.lang == "php" && ctx.class != "" && kind == "method_declaration" && w.phpPublic(fn),
		w.lang == "ruby" && ctx.class != "" && w.rubyPublic(fn, name),
		w.lang == "cpp" && ctx.class != "" && w.cppAPI(ctx):
		s.Exported = true
	}
	s.Trait = ctx.trait
	if w.lang == "cpp" && cppOverrides(fn) {
		s.Annotations = append(s.Annotations, "Override")
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

// phpPublic reports whether a PHP method is public: a public visibility
// modifier, or none (PHP's default).
func (w *multiWalker) phpPublic(fn *tree_sitter.Node) bool {
	for i := uint(0); i < fn.NamedChildCount(); i++ {
		child := fn.NamedChild(i)
		if child != nil && child.Kind() == "visibility_modifier" {
			return strings.TrimSpace(w.nodeText(child)) == "public"
		}
	}
	return true
}

// rubyPublic reports whether a Ruby method is public. An instance method is
// private or protected when its def is the argument of private or protected
// (`private def x`), when its name is (`private :x`), or when the nearest
// bare private, protected or public before it in the class body is not
// public. A singleton method (`def self.x`) is private only through
// private_class_method.
func (w *multiWalker) rubyPublic(fn *tree_sitter.Node, name string) bool {
	singleton := fn.Kind() == "singleton_method"
	stmt := fn
	if p := fn.Parent(); p != nil && p.Kind() == "argument_list" {
		if call := p.Parent(); call != nil && call.Kind() == "call" {
			switch w.rubyCallName(call) {
			case "private", "protected":
				if !singleton {
					return false
				}
			case "private_class_method":
				return false
			}
			stmt = call
		}
	}
	body := stmt.Parent()
	if body == nil {
		return true
	}
	hidden := []string{"private", "protected"}
	if singleton {
		hidden = []string{"private_class_method"}
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		call := body.NamedChild(i)
		if call == nil || call.Kind() != "call" || !slices.Contains(hidden, w.rubyCallName(call)) {
			continue
		}
		if args := call.ChildByFieldName("arguments"); args != nil {
			for j := uint(0); j < args.NamedChildCount(); j++ {
				arg := args.NamedChild(j)
				if arg != nil && strings.TrimPrefix(strings.Trim(w.nodeText(arg), `"'`), ":") == name {
					return false
				}
			}
		}
	}
	if singleton {
		return true
	}
	for prev := stmt.PrevNamedSibling(); prev != nil; prev = prev.PrevNamedSibling() {
		if prev.Kind() != "identifier" {
			continue
		}
		switch w.nodeText(prev) {
		case "private", "protected":
			return false
		case "public":
			return true
		}
	}
	return true
}

// rubyCallName is the method a receiverless Ruby call calls ("private" for
// `private :x`), or "" for a call with a receiver.
func (w *multiWalker) rubyCallName(call *tree_sitter.Node) string {
	if call.ChildByFieldName("receiver") != nil {
		return ""
	}
	if m := call.ChildByFieldName("method"); m != nil {
		return w.nodeText(m)
	}
	return ""
}

// cppAPI reports whether a C++ member in ctx is library API: public, in a
// class declared in a header.
func (w *multiWalker) cppAPI(ctx spanCtx) bool {
	if ctx.hidden {
		return false
	}
	switch strings.ToLower(path.Ext(strings.ReplaceAll(w.path, "\\", "/"))) {
	case ".h", ".hh", ".hpp", ".hxx", ".h++":
		return true
	}
	return false
}

// publicMethod reports whether a JS/TS method is public: no private or
// protected modifier and no #private name.
func (w *multiWalker) publicMethod(fn *tree_sitter.Node) bool {
	if n := fn.ChildByFieldName("name"); n != nil && n.Kind() == "private_property_identifier" {
		return false
	}
	for i := uint(0); i < fn.NamedChildCount(); i++ {
		child := fn.NamedChild(i)
		if child != nil && child.Kind() == "accessibility_modifier" {
			if t := strings.TrimSpace(w.nodeText(child)); t == "private" || t == "protected" {
				return false
			}
		}
	}
	return true
}

// cppOverrides reports a C++ member declaration or definition marked
// virtual, or with an override (or final) specifier.
func cppOverrides(node *tree_sitter.Node) bool {
	for i := uint(0); i < node.ChildCount(); i++ {
		if child := node.Child(i); child != nil && child.Kind() == "virtual" {
			return true
		}
	}
	d := node.ChildByFieldName("declarator")
	for d != nil && d.Kind() != "function_declarator" {
		d = d.ChildByFieldName("declarator")
	}
	if d == nil {
		return false
	}
	for i := uint(0); i < d.NamedChildCount(); i++ {
		if child := d.NamedChild(i); child != nil && child.Kind() == "virtual_specifier" {
			return true
		}
	}
	return false
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
	for _, field := range []string{"superclasses", "superclass", "interfaces", "trait"} {
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

// baseTypeLeaves are the node kinds that name a type in a base list:
// identifier (Python, JS), type_identifier (TS, Java, C++, Rust),
// property_identifier (TS `extends ns.Base`), name (PHP), constant (Ruby).
var baseTypeLeaves = map[string]bool{
	"identifier": true, "type_identifier": true, "property_identifier": true,
	"name": true, "constant": true,
}

// classBaseTypes returns the names of the types a class declaration
// extends or implements: every name in the nodes classHasBases looks at.
// A qualified base (`geo::Base`, `abc.ABC`, `pkg.Base`) contributes each of
// its names, so the last one, the type's own name, is always among them.
func (w *multiWalker) classBaseTypes(node *tree_sitter.Node) []string {
	var names []string
	var collect func(n *tree_sitter.Node)
	collect = func(n *tree_sitter.Node) {
		if n == nil {
			return
		}
		if baseTypeLeaves[n.Kind()] {
			names = append(names, w.nodeText(n))
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			collect(n.NamedChild(i))
		}
	}
	for _, field := range []string{"superclasses", "superclass", "interfaces", "trait"} {
		collect(node.ChildByFieldName(field))
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		switch c := node.NamedChild(i); c.Kind() {
		case "superclass", "super_interfaces", "class_heritage", "base_clause",
			"class_interface_clause", "base_class_clause":
			collect(c)
		}
	}
	return names
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

// declLanguage reports whether code review parses relPath for the class
// member declarations of other files' methods.
func declLanguage(relPath string) bool {
	lang := SupportedLanguage(strings.ToLower(path.Ext(strings.ReplaceAll(relPath, "\\", "/"))))
	return lang != "" && lang != "go" && multiLangGrammars[lang]
}
