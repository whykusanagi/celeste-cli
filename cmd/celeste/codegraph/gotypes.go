package codegraph

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// gotypes.go resolves Go call edges with type information (issue #375).
//
// Every edge from a type-checked file names its endpoints by qualified name
// (types.Func.FullName), so a call to update() in package b can only reach
// b.update, and t.Update() reaches the Update method of t's type. On top of
// direct calls it tracks function values flow-insensitively: a value stored
// in a variable, struct field, map, slice, parameter or result "slot" is
// remembered, and a call through that slot gets an edge to every function
// that may be stored there. Interface methods are symbols of their own, with
// "implements" edges to every module method that implements them.

// goEdge is an edge from the Go pass. Typed endpoints carry a qualified name;
// heuristic endpoints (files that did not type-check) carry a bare name.
type goEdge struct {
	srcQual string // qualified source; empty for heuristic sources
	srcName string // bare source name, used with file when srcQual is empty
	file    string // workspace-relative file of the source
	tgtQual string // qualified target (exact)
	tgtName string // bare/qualified-by-receiver target name (heuristic)
	kind    EdgeKind
}

type goFileResult struct {
	rel        string
	symbols    []Symbol
	resolution string
}

type goResult struct {
	files []goFileResult
	edges []goEdge
	// implements maps a concrete method's qualified name to the interfaces
	// it satisfies, displayed as "pkg.Iface" (or "error").
	implements map[string][]string
}

// analyzeGo type-checks the workspace's Go files and returns their symbols,
// resolution and edges.
func analyzeGo(ctx context.Context, workspace string, relFiles []string) (*goResult, error) {
	l, err := loadGo(ctx, workspace, relFiles)
	if err != nil {
		return nil, err
	}
	a := &goAnalyzer{
		l:       l,
		module:  map[*types.Package]bool{},
		flow:    map[types.Object]*slotNode{},
		edgeSet: map[goEdge]bool{},
	}
	for _, u := range l.units {
		if u.pkg != nil {
			a.module[u.pkg] = true
		}
	}
	for _, u := range append(append([]*goUnit{}, l.tunits...), l.xunits...) {
		if u.pkg != nil {
			a.module[u.pkg] = true
		}
	}

	res := &goResult{}
	p := NewGoParser()
	for _, f := range l.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		syms := p.fileSymbols(f.ast, f.rel, l.fset)
		fr := goFileResult{rel: f.rel, symbols: syms, resolution: GoResolutionApproximate}
		if f.unit != nil && f.unit.info != nil {
			a.qualify(f, syms)
			if f.unit.errs == 0 {
				fr.resolution = GoResolutionTyped
			}
			a.walkFile(f)
		} else {
			for _, decl := range f.ast.Decls {
				if d, ok := decl.(*ast.FuncDecl); ok && d.Body != nil {
					for _, e := range p.extractCallEdges(d, l.fset) {
						a.add(goEdge{srcName: e.SourceName, file: f.rel, tgtName: e.TargetName, kind: e.Kind})
					}
				}
			}
		}
		res.files = append(res.files, fr)
	}
	a.resolveDynamic()
	res.implements = a.interfaces()

	res.edges = make([]goEdge, 0, len(a.edgeSet))
	for e := range a.edgeSet {
		res.edges = append(res.edges, e)
	}
	sort.Slice(res.edges, func(i, j int) bool {
		x, y := res.edges[i], res.edges[j]
		if x.srcQual+x.srcName != y.srcQual+y.srcName {
			return x.srcQual+x.srcName < y.srcQual+y.srcName
		}
		if x.tgtQual+x.tgtName != y.tgtQual+y.tgtName {
			return x.tgtQual+x.tgtName < y.tgtQual+y.tgtName
		}
		return x.kind < y.kind
	})
	return res, nil
}

type goAnalyzer struct {
	l       *goLoader
	module  map[*types.Package]bool
	flow    map[types.Object]*slotNode
	sites   []dynSite
	edgeSet map[goEdge]bool
}

// slotNode is a place a function value can be stored: a variable, struct
// field, parameter or result. funcs are the functions stored directly; succ
// are slots this slot's value flows into.
type slotNode struct {
	funcs  map[*types.Func]bool
	succ   map[types.Object]bool
	solved map[*types.Func]bool
}

// dynSite is a call through a function value, resolved after every flow
// in the module has been seen.
type dynSite struct {
	src string
	set valueSet
}

// valueSet is the function values an expression may evaluate to.
type valueSet struct {
	funcs []*types.Func
	slots []types.Object
}

func (a *goAnalyzer) add(e goEdge) {
	if e.srcQual == "" && e.srcName == "" {
		return
	}
	if e.tgtQual == "" && e.tgtName == "" {
		return
	}
	a.edgeSet[e] = true
}

// edge adds a typed edge to a module object.
func (a *goAnalyzer) edge(src string, tgt types.Object, kind EdgeKind) {
	if src == "" || tgt == nil || !a.module[tgt.Pkg()] {
		return
	}
	if q := qualOf(tgt); q != "" {
		a.add(goEdge{srcQual: src, tgtQual: q, kind: kind})
	}
}

// qualOf returns the qualified name the symbol for obj is stored under, or
// "" for objects that have no symbol (fields, locals, parameters).
func qualOf(obj types.Object) string {
	switch o := obj.(type) {
	case *types.Func:
		return o.Origin().FullName()
	case *types.Var, *types.Const, *types.TypeName:
		if o.Pkg() == nil || o.Parent() != o.Pkg().Scope() {
			return ""
		}
		return o.Pkg().Path() + "." + o.Name()
	}
	return ""
}

// qualify fills Symbol.QualName for a type-checked file's declarations.
func (a *goAnalyzer) qualify(f *goFile, syms []Symbol) {
	info := f.unit.info
	fset := a.l.fset
	key := func(name string, line int, kind SymbolKind) string {
		return name + "\x00" + strconv.Itoa(line) + "\x00" + string(kind)
	}
	quals := map[string]string{}
	set := func(id *ast.Ident, line int, kind SymbolKind) {
		if obj := info.Defs[id]; obj != nil {
			if q := qualOf(obj); q != "" {
				quals[key(id.Name, line, kind)] = q
			}
		}
	}
	for _, decl := range f.ast.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := SymbolFunction
			if d.Recv != nil {
				kind = SymbolMethod
			}
			set(d.Name, fset.Position(d.Pos()).Line, kind)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := SymbolType
					switch s.Type.(type) {
					case *ast.InterfaceType:
						kind = SymbolInterface
					case *ast.StructType:
						kind = SymbolStruct
					}
					set(s.Name, fset.Position(s.Pos()).Line, kind)
					if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
						for _, m := range it.Methods.List {
							for _, n := range m.Names {
								set(n, fset.Position(n.Pos()).Line, SymbolInterfaceMethod)
							}
						}
					}
				case *ast.ValueSpec:
					kind := SymbolVar
					if d.Tok == token.CONST {
						kind = SymbolConst
					}
					for _, n := range s.Names {
						set(n, fset.Position(n.Pos()).Line, kind)
					}
				}
			}
		}
	}
	for i := range syms {
		if q, ok := quals[key(syms[i].Name, syms[i].Line, syms[i].Kind)]; ok {
			syms[i].QualName = q
		}
	}
}

// walkFile records the edges and value flows of one type-checked file.
func (a *goAnalyzer) walkFile(f *goFile) {
	info := f.unit.info
	approx := f.unit.errs > 0
	for _, decl := range f.ast.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			var sig *types.Signature
			src := ""
			if fn, ok := info.Defs[d.Name].(*types.Func); ok {
				src = qualOf(fn)
				sig, _ = fn.Type().(*types.Signature)
			}
			a.walk(info, f.rel, src, d.Name.Name, sig, d.Body, approx)
		case *ast.GenDecl:
			if d.Tok != token.VAR && d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, v := range vs.Values {
					id := vs.Names[0]
					if len(vs.Names) == len(vs.Values) {
						id = vs.Names[i]
					}
					a.walk(info, f.rel, qualOf(info.Defs[id]), id.Name, nil, v, approx)
				}
				a.valueSpec(info, vs)
			}
		}
	}
}

// walk visits one function body (or package-level initializer) whose
// edges belong to the symbol src.
func (a *goAnalyzer) walk(info *types.Info, file, src, srcName string, sig *types.Signature, root ast.Node, approx bool) {
	called := map[*ast.Ident]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			// A closure's calls belong to the enclosing symbol; its returns
			// go to the literal's own (untracked) results.
			a.walk(info, file, src, srcName, nil, x.Body, approx)
			return false
		case *ast.CallExpr:
			a.call(info, file, src, srcName, x, called, approx)
		case *ast.AssignStmt:
			a.assign(info, x)
		case *ast.ValueSpec:
			a.valueSpec(info, x)
		case *ast.RangeStmt:
			if x.Value != nil {
				a.store(a.lhsSlot(info, x.Value), a.sources(info, x.X))
			}
		case *ast.CompositeLit:
			a.compositeFields(info, x)
		case *ast.ReturnStmt:
			if sig != nil && len(x.Results) == sig.Results().Len() {
				for i, r := range x.Results {
					a.store(sig.Results().At(i), a.sources(info, r))
				}
			}
		case *ast.SendStmt:
			a.store(a.lhsSlot(info, x.Chan), a.sources(info, x.Value))
		case *ast.SelectorExpr:
			if called[x.Sel] {
				return true
			}
			called[x.Sel] = true // the Sel ident is handled here, not below
			if sel := info.Selections[x]; sel != nil {
				if sel.Kind() != types.FieldVal {
					a.edge(src, sel.Obj(), EdgeReferences)
				}
			} else if fn, ok := info.Uses[x.Sel].(*types.Func); ok {
				a.edge(src, fn, EdgeReferences)
			}
		case *ast.Ident:
			if called[x] {
				return true
			}
			if fn, ok := info.Uses[x].(*types.Func); ok {
				a.edge(src, fn, EdgeReferences)
			}
		}
		return true
	})
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// calleeExpr strips parentheses and generic instantiation from a call's
// function expression.
func calleeExpr(info *types.Info, fun ast.Expr) ast.Expr {
	fun = unparen(fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		if isFuncExpr(info, ix.X) {
			return unparen(ix.X)
		}
	case *ast.IndexListExpr:
		if isFuncExpr(info, ix.X) {
			return unparen(ix.X)
		}
	}
	return fun
}

func isFuncExpr(info *types.Info, e ast.Expr) bool {
	switch x := unparen(e).(type) {
	case *ast.Ident:
		_, ok := info.Uses[x].(*types.Func)
		return ok
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil {
			return sel.Kind() != types.FieldVal
		}
		_, ok := info.Uses[x.Sel].(*types.Func)
		return ok
	}
	return false
}

// staticCallee returns the function or method a call statically invokes,
// or nil for calls through values, conversions and builtins.
func staticCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	switch f := calleeExpr(info, call.Fun).(type) {
	case *ast.Ident:
		fn, _ := info.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if sel := info.Selections[f]; sel != nil {
			if sel.Kind() == types.FieldVal {
				return nil
			}
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		fn, _ := info.Uses[f.Sel].(*types.Func)
		return fn
	}
	return nil
}

func (a *goAnalyzer) call(info *types.Info, file, src, srcName string, call *ast.CallExpr, called map[*ast.Ident]bool, approx bool) {
	fun := calleeExpr(info, call.Fun)
	if tv, ok := info.Types[unparen(call.Fun)]; ok && tv.IsType() {
		// A conversion T(x) calls nothing; it references the type, as the
		// heuristic parser's T(x) edge always did.
		var tn types.Object
		switch f := fun.(type) {
		case *ast.Ident:
			tn = info.Uses[f]
		case *ast.SelectorExpr:
			tn = info.Uses[f.Sel]
		}
		if tn, ok := tn.(*types.TypeName); ok {
			a.edge(src, tn, EdgeReferences)
		}
		return
	}
	switch f := fun.(type) {
	case *ast.Ident:
		called[f] = true
	case *ast.SelectorExpr:
		called[f.Sel] = true
	}

	if fn := staticCallee(info, call); fn != nil {
		a.edge(src, fn, EdgeCalls)
		a.argFlows(info, fn, call)
		return
	}

	// Builtins, and unresolved names in files that did not type-check.
	var obj types.Object
	switch f := fun.(type) {
	case *ast.Ident:
		obj = info.Uses[f]
		if obj == nil {
			if approx {
				a.heuristic(file, src, srcName, f.Name)
			}
			return
		}
	case *ast.SelectorExpr:
		if info.Selections[f] == nil {
			obj = info.Uses[f.Sel]
			if obj == nil {
				if x, ok := f.X.(*ast.Ident); ok {
					if pn, ok := info.Uses[x].(*types.PkgName); ok {
						// pkg.Missing(): exact when pkg is a module package,
						// otherwise an external call with no symbol.
						if u := a.l.units[pn.Imported().Path()]; u != nil && src != "" {
							a.add(goEdge{srcQual: src, tgtQual: pn.Imported().Path() + "." + f.Sel.Name, kind: EdgeCalls})
						}
						return
					}
				}
				if approx {
					// A method on an expression whose type is unknown.
					a.heuristic(file, src, srcName, f.Sel.Name)
				}
				return
			}
		}
	}
	if _, ok := obj.(*types.Builtin); ok {
		return
	}
	// A call through a package-level variable also counts as a call of
	// that variable, as the heuristic parser always recorded it.
	if v, ok := obj.(*types.Var); ok && qualOf(v) != "" {
		a.edge(src, v, EdgeCalls)
	}
	if src != "" {
		a.sites = append(a.sites, dynSite{src: src, set: a.sources(info, fun)})
	}
}

func (a *goAnalyzer) heuristic(file, srcQual, srcName, tgt string) {
	a.add(goEdge{srcQual: srcQual, srcName: srcName, file: file, tgtName: tgt, kind: EdgeCalls})
}

// argFlows records that each argument's function values flow into the
// callee's parameters. Only module functions matter: their bodies are the
// only ones the analysis sees.
func (a *goAnalyzer) argFlows(info *types.Info, fn *types.Func, call *ast.CallExpr) {
	fn = fn.Origin()
	if !a.module[fn.Pkg()] {
		return
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	params := sig.Params()
	for i, arg := range call.Args {
		var p *types.Var
		switch {
		case sig.Variadic() && i >= params.Len()-1:
			p = params.At(params.Len() - 1)
		case i < params.Len():
			p = params.At(i)
		}
		if p != nil {
			a.store(p, a.sources(info, arg))
		}
	}
}

// sources returns the function values expression e may evaluate to.
func (a *goAnalyzer) sources(info *types.Info, e ast.Expr) valueSet {
	var vs valueSet
	a.collect(info, e, &vs, 0)
	return vs
}

func (a *goAnalyzer) collect(info *types.Info, e ast.Expr, vs *valueSet, depth int) {
	if e == nil || depth > 8 {
		return
	}
	switch x := unparen(e).(type) {
	case *ast.Ident:
		switch o := info.Uses[x].(type) {
		case *types.Func:
			vs.funcs = append(vs.funcs, o)
		case *types.Var:
			vs.slots = append(vs.slots, o)
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil {
			switch o := sel.Obj().(type) {
			case *types.Func:
				vs.funcs = append(vs.funcs, o)
			case *types.Var:
				vs.slots = append(vs.slots, o)
			}
			return
		}
		switch o := info.Uses[x.Sel].(type) {
		case *types.Func:
			vs.funcs = append(vs.funcs, o)
		case *types.Var:
			vs.slots = append(vs.slots, o)
		}
	case *ast.IndexExpr:
		if isFuncExpr(info, x.X) {
			a.collect(info, x.X, vs, depth+1)
			return
		}
		a.collect(info, x.X, vs, depth+1) // element of a container slot
	case *ast.IndexListExpr:
		a.collect(info, x.X, vs, depth+1)
	case *ast.SliceExpr:
		a.collect(info, x.X, vs, depth+1)
	case *ast.StarExpr:
		a.collect(info, x.X, vs, depth+1)
	case *ast.TypeAssertExpr:
		a.collect(info, x.X, vs, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.ARROW || x.Op == token.AND {
			a.collect(info, x.X, vs, depth+1)
		}
	case *ast.CompositeLit:
		if t := info.Types[x].Type; t != nil {
			switch t.Underlying().(type) {
			case *types.Map, *types.Slice, *types.Array:
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						a.collect(info, kv.Value, vs, depth+1)
					} else {
						a.collect(info, el, vs, depth+1)
					}
				}
			}
		}
	case *ast.CallExpr:
		if id, ok := unparen(x.Fun).(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "append" {
				for _, arg := range x.Args {
					a.collect(info, arg, vs, depth+1)
				}
				return
			}
		}
		if fn := staticCallee(info, x); fn != nil {
			if sig, ok := fn.Origin().Type().(*types.Signature); ok {
				for i := 0; i < sig.Results().Len(); i++ {
					vs.slots = append(vs.slots, sig.Results().At(i))
				}
			}
		}
	}
}

// lhsSlot returns the slot an assignment target writes to.
func (a *goAnalyzer) lhsSlot(info *types.Info, e ast.Expr) types.Object {
	switch x := unparen(e).(type) {
	case *ast.Ident:
		if x.Name == "_" {
			return nil
		}
		if v, ok := info.Defs[x].(*types.Var); ok {
			return v
		}
		if v, ok := info.Uses[x].(*types.Var); ok {
			return v
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil {
			if sel.Kind() == types.FieldVal {
				return sel.Obj()
			}
			return nil
		}
		if v, ok := info.Uses[x.Sel].(*types.Var); ok {
			return v
		}
	case *ast.IndexExpr:
		return a.lhsSlot(info, x.X)
	case *ast.StarExpr:
		return a.lhsSlot(info, x.X)
	}
	return nil
}

func (a *goAnalyzer) assign(info *types.Info, x *ast.AssignStmt) {
	if x.Tok != token.ASSIGN && x.Tok != token.DEFINE {
		return
	}
	a.assignPairs(info, x.Lhs, x.Rhs, func(e ast.Expr) types.Object { return a.lhsSlot(info, e) })
}

func (a *goAnalyzer) valueSpec(info *types.Info, vs *ast.ValueSpec) {
	lhs := make([]ast.Expr, len(vs.Names))
	for i, n := range vs.Names {
		lhs[i] = n
	}
	a.assignPairs(info, lhs, vs.Values, func(e ast.Expr) types.Object { return a.lhsSlot(info, e) })
}

func (a *goAnalyzer) assignPairs(info *types.Info, lhs, rhs []ast.Expr, slot func(ast.Expr) types.Object) {
	if len(lhs) == len(rhs) {
		for i := range lhs {
			a.store(slot(lhs[i]), a.sources(info, rhs[i]))
		}
		return
	}
	if len(rhs) != 1 {
		return
	}
	if call, ok := unparen(rhs[0]).(*ast.CallExpr); ok {
		if fn := staticCallee(info, call); fn != nil {
			if sig, ok := fn.Origin().Type().(*types.Signature); ok {
				for i := 0; i < len(lhs) && i < sig.Results().Len(); i++ {
					a.store(slot(lhs[i]), valueSet{slots: []types.Object{sig.Results().At(i)}})
				}
			}
		}
		return
	}
	// v, ok := m[k] / x.(T) / <-ch
	if len(lhs) > 0 {
		a.store(slot(lhs[0]), a.sources(info, rhs[0]))
	}
}

// compositeFields records struct literal field values: T{Field: fn}.
func (a *goAnalyzer) compositeFields(info *types.Info, lit *ast.CompositeLit) {
	t := info.Types[lit].Type
	if t == nil {
		return
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok {
				if v, ok := info.Uses[key].(*types.Var); ok {
					a.store(v, a.sources(info, kv.Value))
				}
			}
			continue
		}
		if i < st.NumFields() {
			a.store(st.Field(i), a.sources(info, el))
		}
	}
}

func canonical(o types.Object) types.Object {
	if v, ok := o.(*types.Var); ok {
		return v.Origin()
	}
	return o
}

func (a *goAnalyzer) node(o types.Object) *slotNode {
	o = canonical(o)
	n := a.flow[o]
	if n == nil {
		n = &slotNode{funcs: map[*types.Func]bool{}, succ: map[types.Object]bool{}}
		a.flow[o] = n
	}
	return n
}

// store records that the values in vs flow into dst.
func (a *goAnalyzer) store(dst types.Object, vs valueSet) {
	if dst == nil || (len(vs.funcs) == 0 && len(vs.slots) == 0) {
		return
	}
	dst = canonical(dst)
	d := a.node(dst)
	for _, fn := range vs.funcs {
		d.funcs[fn.Origin()] = true
	}
	for _, s := range vs.slots {
		if s = canonical(s); s != dst {
			a.node(s).succ[dst] = true
		}
	}
}

// resolveDynamic propagates function values through the slot graph and
// adds a calls edge from each dynamic call site to every function that can
// reach it.
func (a *goAnalyzer) resolveDynamic() {
	var queue []types.Object
	for o, n := range a.flow {
		n.solved = map[*types.Func]bool{}
		for fn := range n.funcs {
			n.solved[fn] = true
		}
		if len(n.solved) > 0 {
			queue = append(queue, o)
		}
	}
	for len(queue) > 0 {
		o := queue[0]
		queue = queue[1:]
		n := a.flow[o]
		for s := range n.succ {
			sn := a.flow[s]
			changed := false
			for fn := range n.solved {
				if !sn.solved[fn] {
					sn.solved[fn] = true
					changed = true
				}
			}
			if changed {
				queue = append(queue, s)
			}
		}
	}
	for _, site := range a.sites {
		for _, fn := range site.set.funcs {
			a.edge(site.src, fn, EdgeCalls)
		}
		for _, s := range site.set.slots {
			if n := a.flow[canonical(s)]; n != nil {
				for fn := range n.solved {
					a.edge(site.src, fn, EdgeCalls)
				}
			}
		}
	}
}

// interfaces adds implements edges from module interface methods to the
// module methods that implement them, and returns, for every module method,
// the interfaces (module or imported, plus error) it satisfies.
func (a *goAnalyzer) interfaces() map[string][]string {
	type iface struct {
		named *types.Named
		it    *types.Interface
		local bool
	}
	var ifaces []iface
	var concrete []*types.Named
	seenImport := map[*types.Package]bool{}

	addIface := func(obj types.Object, local bool) {
		tn, ok := obj.(*types.TypeName)
		if !ok || tn.IsAlias() {
			return
		}
		named, ok := tn.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 {
			return
		}
		it, ok := named.Underlying().(*types.Interface)
		if !ok || it.NumMethods() == 0 || !it.IsMethodSet() {
			return
		}
		ifaces = append(ifaces, iface{named, it, local})
	}

	pkgs := make([]*types.Package, 0, len(a.module))
	for p := range a.module {
		pkgs = append(pkgs, p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Path() < pkgs[j].Path() })
	for _, p := range pkgs {
		scope := p.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			addIface(obj, true)
			if tn, ok := obj.(*types.TypeName); ok && !tn.IsAlias() {
				if named, ok := tn.Type().(*types.Named); ok && named.TypeParams().Len() == 0 {
					if _, isIface := named.Underlying().(*types.Interface); !isIface {
						concrete = append(concrete, named)
					}
				}
			}
		}
		for _, imp := range p.Imports() {
			if a.module[imp] || seenImport[imp] {
				continue
			}
			seenImport[imp] = true
			scope := imp.Scope()
			for _, name := range scope.Names() {
				if token.IsExported(name) {
					addIface(scope.Lookup(name), false)
				}
			}
		}
	}
	addIface(types.Universe.Lookup("error"), false)

	// Index concrete types by the method names of *T's method set.
	byMethod := map[string][]*types.Named{}
	for _, t := range concrete {
		if _, ok := t.Underlying().(*types.Pointer); ok {
			continue
		}
		ms := types.NewMethodSet(types.NewPointer(t))
		for i := 0; i < ms.Len(); i++ {
			name := ms.At(i).Obj().Name()
			byMethod[name] = append(byMethod[name], t)
		}
	}

	qualifier := func(p *types.Package) string { return p.Name() }
	impl := map[string]map[string]bool{}
	for _, in := range ifaces {
		display := types.TypeString(in.named, qualifier)
		for _, t := range byMethod[in.it.Method(0).Name()] {
			ptr := types.NewPointer(t)
			if !types.Implements(ptr, in.it) {
				continue
			}
			for i := 0; i < in.it.NumMethods(); i++ {
				m := in.it.Method(i)
				obj, _, _ := types.LookupFieldOrMethod(ptr, false, m.Pkg(), m.Name())
				fn, ok := obj.(*types.Func)
				if !ok || !a.module[fn.Pkg()] {
					continue
				}
				if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
					continue // promoted from an embedded interface: not a concrete method
				}
				q := qualOf(fn)
				if impl[q] == nil {
					impl[q] = map[string]bool{}
				}
				impl[q][display] = true
				if in.local {
					a.edge(qualOf(m), fn, EdgeImplements)
				}
			}
		}
	}

	out := make(map[string][]string, len(impl))
	for q, set := range impl {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out[q] = names
	}
	return out
}

// implementsList joins interface names for symbols.implements.
func implementsList(names []string) string {
	return strings.Join(names, ",")
}
