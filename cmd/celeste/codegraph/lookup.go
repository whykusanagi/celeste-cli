package codegraph

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// MatchKind says how LookupSymbol matched a query (#395).
type MatchKind int

const (
	// MatchNone: nothing matched.
	MatchNone MatchKind = iota
	// MatchExact: symbols whose name is the query.
	MatchExact
	// MatchQualified: symbols whose qualified name is the query, such as
	// "(tui.AppModel).update", "commands.Execute" or "core.add".
	MatchQualified
	// MatchExactFold: symbols whose name is the query, ignoring case.
	MatchExactFold
	// MatchPartial: symbols whose name starts with or contains the query
	// (prefix matches first), or, for a qualified query nothing matched,
	// the symbols named by its last part. These are candidates, not the
	// symbol asked for.
	MatchPartial
)

// LookupResult is the outcome of LookupSymbol: every symbol of the best
// tier that matched, non-test symbols first.
type LookupResult struct {
	Match   MatchKind
	Symbols []Symbol
}

// LookupSymbol finds the symbol a query names. It tries, in order: an exact
// name; a qualified name (the forms QualifiedName and DisplayName print, a
// Go import path or package, a receiver type, or for other languages the
// file's stem or path); the name ignoring case; then names that start with
// or contain the query. The first tier with a match is returned whole, so
// a common name gives every symbol that has it rather than the first few
// in alphabetical order.
//
// A trailing ":line" ("pkg/core.add:9", as QualifiedNames prints for
// same-named symbols in one file) keeps the symbols declared on that line.
// When none is, the symbols the rest of the query found are returned as
// MatchPartial candidates.
func (s *Store) LookupSymbol(query string) (LookupResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return LookupResult{}, nil
	}
	if rest, line, ok := splitLine(q); ok {
		res, err := s.lookupSymbol(rest)
		if err != nil || res.Match == MatchNone {
			return res, err
		}
		var hits []Symbol
		for _, sym := range res.Symbols {
			if sym.Line == line {
				hits = append(hits, sym)
			}
		}
		if len(hits) == 0 {
			return LookupResult{Match: MatchPartial, Symbols: res.Symbols}, nil
		}
		return LookupResult{Match: res.Match, Symbols: hits}, nil
	}
	return s.lookupSymbol(q)
}

// splitLine splits "name:12" into "name" and 12.
func splitLine(q string) (string, int, bool) {
	i := strings.LastIndex(q, ":")
	if i <= 0 || i == len(q)-1 {
		return "", 0, false
	}
	line, err := strconv.Atoi(q[i+1:])
	if err != nil || line <= 0 || strings.ContainsAny(q[i+1:], "+-") {
		return "", 0, false
	}
	return strings.TrimSpace(q[:i]), line, true
}

// lookupSymbol is LookupSymbol without the ":line" form; q is trimmed.
func (s *Store) lookupSymbol(q string) (LookupResult, error) {
	syms, err := s.symbolsNamed(q, false)
	if err != nil {
		return LookupResult{}, err
	}
	if len(syms) > 0 {
		return LookupResult{Match: MatchExact, Symbols: rankExact(syms)}, nil
	}
	qual, name, qualified := splitQualified(q)
	if qualified {
		// A C++ member defined out of line is stored under its qualified
		// name ("Shape::make"); "Shape.make" names it too.
		if !strings.Contains(q, "::") && !strings.HasPrefix(q, "(") {
			if syms, err = s.symbolsNamed(strings.ReplaceAll(qual, ".", "::")+"::"+name, false); err != nil {
				return LookupResult{}, err
			}
			if len(syms) > 0 {
				return LookupResult{Match: MatchQualified, Symbols: rankExact(syms)}, nil
			}
		}
		for _, fold := range []bool{false, true} {
			cands, err := s.symbolsNamed(name, fold)
			if err != nil {
				return LookupResult{}, err
			}
			var hits []Symbol
			for _, c := range cands {
				if qualifierMatches(c, qual, fold) {
					hits = append(hits, c)
				}
			}
			if len(hits) > 0 {
				return LookupResult{Match: MatchQualified, Symbols: rankExact(hits)}, nil
			}
		}
	}
	if syms, err = s.symbolsNamed(q, true); err != nil {
		return LookupResult{}, err
	}
	if len(syms) > 0 {
		return LookupResult{Match: MatchExactFold, Symbols: rankExact(syms)}, nil
	}
	if syms, err = s.SearchSymbolsByName(q); err != nil {
		return LookupResult{}, err
	}
	if len(syms) > 0 {
		return LookupResult{Match: MatchPartial, Symbols: rankPartial(syms, q)}, nil
	}
	if qualified {
		// The qualifier matched nothing; offer what carries the name.
		for _, fold := range []bool{false, true} {
			if syms, err = s.symbolsNamed(name, fold); err != nil {
				return LookupResult{}, err
			}
			if len(syms) > 0 {
				return LookupResult{Match: MatchPartial, Symbols: rankExact(syms)}, nil
			}
		}
	}
	return LookupResult{}, nil
}

// RankedSearch is keyword search over symbol names: LookupSymbol's best
// tier first, then the remaining names that contain the query (prefix
// matches first, non-test symbols first), cut to limit.
func (s *Store) RankedSearch(query string, limit int) ([]Symbol, error) {
	res, err := s.LookupSymbol(query)
	if err != nil {
		return nil, err
	}
	out := res.Symbols
	if res.Match != MatchPartial {
		more, err := s.SearchSymbolsByName(strings.TrimSpace(query))
		if err != nil {
			return nil, err
		}
		seen := make(map[int64]bool, len(out))
		for _, sym := range out {
			seen[sym.ID] = true
		}
		for _, sym := range rankPartial(more, strings.TrimSpace(query)) {
			if !seen[sym.ID] {
				out = append(out, sym)
			}
		}
	}
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// QualifiedName is a name for sym that LookupSymbol accepts and that tells
// same-named symbols apart: "(tui.AppModel).update" for a Go method,
// "commands.Execute" for another Go symbol, "core.add" (file stem) for a
// symbol of another language, "core.Foo.add" for its method of class Foo.
func QualifiedName(sym Symbol) string {
	if sym.QualName == "" && DetectLanguage(sym.File) != "go" {
		name := sym.Name
		if sym.Scope != "" {
			name = sym.Scope + "." + name
		}
		if stem := fileStem(slashFile(sym.File)); stem != "" && stem != "." {
			return stem + "." + name
		}
		return name
	}
	if sym.Kind == SymbolMethod || sym.Kind == SymbolInterfaceMethod {
		if d := DisplayName(sym); d != sym.Name {
			return d
		}
		if recv := sigReceiver(sym.Signature); recv != "" {
			star := ""
			if strings.HasPrefix(recv, "*") {
				star, recv = "*", recv[1:]
			}
			if sym.Package != "" {
				recv = sym.Package + "." + recv
			}
			return "(" + star + recv + ")." + sym.Name
		}
	}
	if pkg := qualPackage(sym.QualName); pkg != "" {
		return path.Base(pkg) + "." + sym.Name
	}
	if sym.Package != "" {
		return sym.Package + "." + sym.Name
	}
	return sym.Name
}

// QualifiedNames names each of syms as QualifiedName does, except that
// symbols whose short names collide (two packages named util, two files
// named core.py) get a longer name LookupSymbol still accepts: the full Go
// qualified name, or the file path without its extension. Symbols that
// still collide (a function and a method of one name in one file) also get
// their line, "pkg/core.add:9".
func QualifiedNames(syms []Symbol) []string {
	names := make([]string, len(syms))
	count := map[string]int{}
	for i, sym := range syms {
		names[i] = QualifiedName(sym)
		count[names[i]]++
	}
	long := map[string]int{}
	for i, sym := range syms {
		if count[names[i]] > 1 {
			names[i] = longQualifiedName(sym)
			long[names[i]]++
		}
	}
	for i, sym := range syms {
		if long[names[i]] > 1 {
			names[i] += ":" + strconv.Itoa(sym.Line)
		}
	}
	return names
}

// longQualifiedName is sym's most specific name LookupSymbol accepts.
func longQualifiedName(sym Symbol) string {
	if q := sym.QualName; q != "" && !strings.Contains(q, "#") {
		return q
	}
	file := slashFile(sym.File)
	name := sym.Name
	if sym.Scope != "" && DetectLanguage(sym.File) != "go" {
		name = sym.Scope + "." + name
	}
	return strings.TrimSuffix(file, path.Ext(file)) + "." + name
}

// slashFile is an indexed path with '/' separators. The index stores
// filepath.Rel paths, which use '\' on Windows; qualifiers and printed
// names always use '/'.
func slashFile(file string) string {
	return strings.ReplaceAll(file, `\`, "/")
}

// symbolsNamed returns the symbols whose name is name, ignoring case when
// fold is set.
func (s *Store) symbolsNamed(name string, fold bool) ([]Symbol, error) {
	where := `name = ?`
	if fold {
		where = `name = ? COLLATE NOCASE`
	}
	rows, err := s.db.Query(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, ''),
		        COALESCE(decorators, ''), COALESCE(base_classes, ''),
		        COALESCE(qual_name, ''), COALESCE(implements, ''), COALESCE(scope, '')
		 FROM symbols WHERE `+where+` ORDER BY file, line, id`, name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// splitQualified splits "(*pkg.T).M", "pkg.T.M", "pkg/path.F" or "mod.f"
// into its qualifier and the name after the last dot. ok is false for a
// plain name.
func splitQualified(q string) (qual, name string, ok bool) {
	// A C++/PHP/Ruby scope ("Geo::Qux::add", "Baz::add") reads as dots:
	// the qualifier is the class chain before the last "::".
	if i := strings.LastIndex(q, "::"); i >= 0 && !strings.HasPrefix(q, "(") {
		q = strings.ReplaceAll(q[:i], "::", ".") + "." + q[i+2:]
	}
	if strings.HasPrefix(q, "(") {
		end := strings.LastIndex(q, ").")
		if end < 0 {
			return "", "", false
		}
		qual, name = q[1:end], q[end+2:]
	} else {
		dot := strings.LastIndex(q, ".")
		if dot < 0 {
			return "", "", false
		}
		qual, name = q[:dot], q[dot+1:]
	}
	qual = stripTypeParams(strings.TrimPrefix(strings.TrimSpace(qual), "*"))
	name = strings.TrimSpace(name)
	if qual == "" || name == "" {
		return "", "", false
	}
	return qual, name, true
}

// qualifierMatches reports whether qual names a scope sym is declared in.
// Any trailing part of a scope counts when it starts at a '/' or '.': a
// receiver "example.com/m/tui.AppModel" accepts "tui.AppModel" and
// "AppModel", an import path accepts its last elements, a file path
// "pkg/core.py" accepts "core", "pkg/core" and "pkg.core".
func qualifierMatches(sym Symbol, qual string, fold bool) bool {
	for _, scope := range symbolScopes(sym) {
		if boundarySuffix(scope, qual, fold) {
			return true
		}
	}
	return false
}

// symbolScopes lists the scopes a qualifier may name for sym.
func symbolScopes(sym Symbol) []string {
	var scopes []string
	add := func(s string) {
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	file := slashFile(sym.File)
	noExt := strings.TrimSuffix(file, path.Ext(file))
	if DetectLanguage(sym.File) != "go" && sym.Scope != "" {
		// A member of a class is qualified by the class, alone or after
		// its file ("Foo.add", "core.Foo.add", "pkg/core.Foo.add"); the
		// file alone names the file's top-level symbols.
		add(sym.Scope)
		add(noExt + "." + sym.Scope)
		add(strings.ReplaceAll(noExt, "/", ".") + "." + sym.Scope)
		return scopes
	}
	add(noExt)
	add(strings.ReplaceAll(noExt, "/", "."))
	if DetectLanguage(sym.File) != "go" {
		return scopes
	}
	add(sym.Package)
	q := sym.QualName
	if strings.HasPrefix(q, "(") {
		if end := strings.LastIndex(q, ")."); end > 0 {
			recv := stripTypeParams(strings.TrimPrefix(q[1:end], "*"))
			add(recv)
			if dot := strings.LastIndex(recv, "."); dot > 0 {
				add(recv[:dot]) // the import path alone
			}
		}
	} else {
		add(qualPackage(q))
	}
	if q == "" {
		if recv := strings.TrimPrefix(sigReceiver(sym.Signature), "*"); recv != "" {
			add(recv)
			if sym.Package != "" {
				add(sym.Package + "." + recv)
			}
		}
	}
	return scopes
}

// qualPackage is the import path of a Go qualified name that has no
// receiver, "example.com/m/commands" for "example.com/m/commands.Execute".
func qualPackage(q string) string {
	if q == "" || strings.HasPrefix(q, "(") {
		return ""
	}
	if i := strings.Index(q, "#"); i >= 0 {
		q = q[:i] // init functions are keyed by file
	}
	if dot := strings.LastIndex(q, "."); dot > 0 {
		return q[:dot]
	}
	return ""
}

// sigReceiver is the receiver type of a Go method signature,
// "*Model" for "func (m *Model) view() string", without type parameters.
func sigReceiver(sig string) string {
	if !strings.HasPrefix(sig, "func (") {
		return ""
	}
	end := strings.Index(sig, ")")
	if end < 0 {
		return ""
	}
	fields := strings.Fields(sig[len("func ("):end])
	if len(fields) == 0 {
		return ""
	}
	return stripTypeParams(fields[len(fields)-1])
}

// stripTypeParams drops a trailing type-parameter list: "Cache[K, V]" ->
// "Cache".
func stripTypeParams(s string) string {
	if i := strings.Index(s, "["); i > 0 {
		return s[:i]
	}
	return s
}

// boundarySuffix reports whether s ends with suffix where that suffix is
// all of s or starts right after a '/' or '.'.
func boundarySuffix(s, suffix string, fold bool) bool {
	if fold {
		s, suffix = strings.ToLower(s), strings.ToLower(suffix)
	}
	if !strings.HasSuffix(s, suffix) {
		return false
	}
	if len(s) == len(suffix) {
		return true
	}
	c := s[len(s)-len(suffix)-1]
	return c == '/' || c == '.'
}

func fileStem(file string) string {
	base := path.Base(file)
	return strings.TrimSuffix(base, path.Ext(base))
}

// rankExact orders same-named symbols: non-test before test, then by file
// and line.
func rankExact(syms []Symbol) []Symbol {
	sort.SliceStable(syms, func(i, j int) bool {
		ti, tj := isTestFilePath(syms[i].File), isTestFilePath(syms[j].File)
		if ti != tj {
			return !ti
		}
		if syms[i].File != syms[j].File {
			return syms[i].File < syms[j].File
		}
		return syms[i].Line < syms[j].Line
	})
	return syms
}

// rankPartial orders partial matches: non-test first, then names that
// start with q (ignoring case), then shorter names, then by name, file and
// line.
func rankPartial(syms []Symbol, q string) []Symbol {
	lq := strings.ToLower(q)
	sort.SliceStable(syms, func(i, j int) bool {
		a, b := syms[i], syms[j]
		ta, tb := isTestFilePath(a.File), isTestFilePath(b.File)
		if ta != tb {
			return !ta
		}
		pa, pb := strings.HasPrefix(strings.ToLower(a.Name), lq), strings.HasPrefix(strings.ToLower(b.Name), lq)
		if pa != pb {
			return pa
		}
		if len(a.Name) != len(b.Name) {
			return len(a.Name) < len(b.Name)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return syms
}
