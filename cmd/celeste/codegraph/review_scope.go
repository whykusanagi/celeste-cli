package codegraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strings"
)

// Code review scopes each function's findings to the function's own lines.
// The spans come from the parsers: go/ast for Go (functions and methods),
// tree-sitter for the languages it parses in cgo builds (TypeScript,
// JavaScript, PHP, Python, Java, C, C++, Ruby, Rust). A function the parser
// gives no span for (the regex parser of a CGO_ENABLED=0 build, a file that
// no longer parses) falls back to a text scan from its definition line:
// braces for brace languages, indentation for Python, the matching `end`
// for Ruby. Every finding's line is the 1-based line it is on.

// funcSpan is the extent of one function or method in its file. Lines are
// 1-based and inclusive; Start is the line the index records for the
// function (Symbol.Line).
type funcSpan struct {
	Name  string
	Start int
	End   int
	// HasBody is false for a declaration without a body: an interface or
	// abstract method.
	HasBody bool
	// Stmts holds the source of each top-level statement of the body,
	// comments excluded; Comments holds the comments inside the function.
	Stmts    []string
	Comments []string
	// Class is the enclosing class, or the receiver type of a Go method;
	// ClassHasBases is true when that class extends or implements a type.
	Class         string
	ClassHasBases bool
	// Constructor is true for a constructor: a Java or C++ constructor, a
	// Ruby initialize, a PHP __construct, a JS/TS constructor.
	Constructor bool
	// Exported is true for the exported API of a library: an exported Go
	// function or method outside package main, an exported JS/TS function.
	Exported bool
	// Annotations lists Java annotation names (Override, Test).
	Annotations []string
	// Exact is true when the span comes from a parser rather than the
	// text-scan fallback.
	Exact bool
}

// numberedLine is one source line and its 1-based line number.
type numberedLine struct {
	n    int
	text string
}

// reviewFile is a source file prepared for review: its lines and the spans
// of its functions.
type reviewFile struct {
	lang  string
	src   []byte
	lines []string
	spans []funcSpan
	// Go only: the file has build constraints (a //go:build line or a
	// GOOS/GOARCH file name suffix), and the file is in package main.
	constrained bool
	goMain      bool
}

// reviewer prepares files for review. It owns a tree-sitter parser in cgo
// builds, created on first use; close releases it.
type reviewer struct {
	ts *MultiLangParser
}

func (r *reviewer) close() {
	if r.ts != nil {
		r.ts.Close()
		r.ts = nil
	}
}

// load parses src (the content of relPath) for its function spans.
func (r *reviewer) load(relPath string, src []byte) *reviewFile {
	f := &reviewFile{
		lang:  DetectLanguage(relPath),
		src:   src,
		lines: strings.Split(string(src), "\n"),
	}
	if f.lang == "go" {
		f.constrained = goBuildConstrained(relPath, src)
		if spans, isMain, ok := goFuncSpans(src); ok {
			f.spans, f.goMain = spans, isMain
		}
		return f
	}
	if spans, ok := r.treeSitterSpans(relPath, src); ok {
		f.spans = spans
	}
	return f
}

// span returns the span of the function c: the parser's span with c's name
// that starts on c's line, else the text-scan fallback from c's line.
func (f *reviewFile) span(c FunctionEdgeInfo) funcSpan {
	for _, s := range f.spans {
		if s.Start == c.Line && s.Name == c.Name {
			return s
		}
	}
	for _, s := range f.spans {
		if s.Start == c.Line {
			return s
		}
	}
	return fallbackSpan(f.lines, f.lang, c.Line, c.Name)
}

// bodyLines returns the lines of span s with their numbers, leaving out the
// lines of any other function nested inside it: a finding in a nested
// function belongs to that function.
func (f *reviewFile) bodyLines(s funcSpan) []numberedLine {
	var out []numberedLine
	for n := s.Start; n <= s.End && n <= len(f.lines); n++ {
		if n < 1 || f.nestedAt(s, n) {
			continue
		}
		out = append(out, numberedLine{n: n, text: f.lines[n-1]})
	}
	return out
}

func (f *reviewFile) nestedAt(outer funcSpan, n int) bool {
	for _, o := range f.spans {
		if o.Start > outer.Start && o.End <= outer.End && n >= o.Start && n <= o.End {
			return true
		}
	}
	return false
}

// joinLines joins the text of numbered lines.
func joinLines(lines []numberedLine) string {
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = l.text
	}
	return strings.Join(parts, "\n")
}

// goFuncSpans returns the span of every function and method declared in a
// Go file, and whether the file is in package main.
func goFuncSpans(src []byte) ([]funcSpan, bool, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil || file == nil {
		return nil, false, false
	}
	isMain := file.Name.Name == "main"
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	var spans []funcSpan
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		s := funcSpan{
			Name:    fn.Name.Name,
			Start:   fset.Position(fn.Pos()).Line,
			End:     fset.Position(fn.End()).Line,
			HasBody: fn.Body != nil,
			Exact:   true,
		}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			s.Class = goRecvName(fn.Recv.List[0].Type)
		}
		s.Exported = !isMain && ast.IsExported(s.Name) && (s.Class == "" || ast.IsExported(s.Class))
		if fn.Body != nil {
			for _, st := range fn.Body.List {
				if from, to := offset(st.Pos()), offset(st.End()); from >= 0 && to <= len(src) && from < to {
					s.Stmts = append(s.Stmts, string(src[from:to]))
				}
			}
			for _, cg := range file.Comments {
				if cg.Pos() > fn.Body.Lbrace && cg.End() < fn.Body.Rbrace {
					for _, c := range cg.List {
						s.Comments = append(s.Comments, c.Text)
					}
				}
			}
		}
		spans = append(spans, s)
	}
	return spans, isMain, true
}

// goRecvName returns the base type name of a method receiver.
func goRecvName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// goBuildConstrained reports whether a Go file builds only under some
// configurations: it has a //go:build (or // +build) line before its package
// clause, or its name ends in a GOOS or GOARCH suffix.
func goBuildConstrained(relPath string, src []byte) bool {
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		if strings.HasPrefix(trimmed, "//go:build") || strings.HasPrefix(trimmed, "// +build") {
			return true
		}
	}
	name := strings.TrimSuffix(path.Base(strings.ReplaceAll(relPath, "\\", "/")), ".go")
	name = strings.TrimSuffix(name, "_test")
	parts := strings.Split(name, "_")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if knownGOOS[last] || knownGOARCH[last] {
		return true
	}
	return false
}

// knownGOOS and knownGOARCH are the file name suffixes the go tool treats
// as build constraints.
var knownGOOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "js": true, "linux": true, "nacl": true,
	"netbsd": true, "openbsd": true, "plan9": true, "solaris": true, "wasip1": true,
	"windows": true, "zos": true,
}

var knownGOARCH = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "arm64": true,
	"arm64be": true, "armbe": true, "loong64": true, "mips": true, "mips64": true,
	"mips64le": true, "mips64p32": true, "mips64p32le": true, "mipsle": true,
	"ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true,
	"s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

// fallbackLines bounds how far the text-scan fallback looks for the end of
// a function.
const fallbackLines = 2000

// fallbackSpan scans the text from a function's definition line for its end
// when no parser span is available: indentation for Python, the matching
// `end` for Ruby, braces for the other languages.
func fallbackSpan(lines []string, lang string, start int, name string) funcSpan {
	s := funcSpan{Name: name, Start: start, End: start, HasBody: true}
	if start < 1 || start > len(lines) {
		return s
	}
	end := start - 1 + fallbackLines
	if end > len(lines) {
		end = len(lines)
	}
	window := strings.Join(lines[start-1:end], "\n")
	var text string
	switch lang {
	case "python":
		text = extractIndentedBody(window)
	case "ruby":
		text = rubyMethodText(window)
	default:
		text = extractBracedBody(window, lang)
	}
	s.End = start + strings.Count(text, "\n")
	for s.End > s.Start && strings.TrimSpace(lines[s.End-1]) == "" {
		s.End--
	}
	s.HasBody, s.Stmts, s.Comments = fallbackBody(lines[s.Start-1:s.End], lang)
	return s
}

// rubyMethodText trims a window starting at a Ruby def to the def itself:
// up to the `end` at the def's indentation, or the def line alone when it
// ends on that line.
func rubyMethodText(window string) string {
	lines := strings.Split(window, "\n")
	first := strings.TrimSpace(lines[0])
	if strings.HasSuffix(first, " end") || strings.HasSuffix(first, ";end") {
		return lines[0]
	}
	indent := countLeadingSpaces(lines[0])
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "end" && countLeadingSpaces(lines[i]) == indent {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	return window
}

// fallbackBody splits the text of a function found by the text scan into
// body statements and comments, one statement per line.
func fallbackBody(fnLines []string, lang string) (bool, []string, []string) {
	text := strings.Join(fnLines, "\n")
	var inner string
	switch lang {
	case "python":
		// Everything after the signature's closing ':'.
		depth, cut := 0, -1
		for i, ch := range text {
			switch ch {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			case ':':
				if depth == 0 && cut < 0 {
					cut = i + 1
				}
			}
		}
		if cut < 0 {
			return true, nil, nil
		}
		inner = text[cut:]
	case "ruby":
		inner = strings.Join(fnLines[1:], "\n")
		if len(fnLines) == 1 {
			if i := strings.Index(text, ";"); i >= 0 {
				inner = text[i+1:]
			} else {
				inner = ""
			}
		}
		inner = strings.TrimSuffix(strings.TrimSpace(inner), "end")
	default:
		open, shut := strings.Index(text, "{"), strings.LastIndex(text, "}")
		if open < 0 || shut < open {
			return false, nil, nil
		}
		inner = text[open+1 : shut]
	}
	var stmts, comments []string
	inBlock := false
	for _, line := range strings.Split(inner, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
		case inBlock:
			comments = append(comments, t)
			if strings.Contains(t, "*/") {
				inBlock = false
			}
		case strings.HasPrefix(t, "/*"):
			comments = append(comments, t)
			inBlock = !strings.Contains(t, "*/")
		case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "#"):
			comments = append(comments, t)
		default:
			stmts = append(stmts, t)
		}
	}
	return true, stmts, comments
}

// The three kinds of stub body.
const (
	stubEmpty   = "empty body"
	stubTodo    = "body holds only a TODO/FIXME comment"
	stubNotImpl = `body only raises "not implemented"`
)

// stubBody classifies a function body. A stub body is exactly one of:
//   - empty: no statements and no comments ({}, pass, ..., a docstring);
//   - TODO-only: no statements, and its comments carry a TODO, FIXME, XXX
//     or HACK marker;
//   - not implemented: its only statements raise "not implemented"
//     (panic("not implemented"), raise NotImplementedError,
//     throw new Error("not implemented"), UnsupportedOperationException,
//     unimplemented!(), todo!()).
//
// Any other statement (a return of a literal, a call, an assignment) makes
// the body real, and so does a comment-only body without a work marker (a
// documented no-op). A declaration without a body is not a stub body. It
// returns "" for a body that is not a stub.
func stubBody(s funcSpan) string {
	if !s.HasBody {
		return ""
	}
	notImpl := false
	for _, st := range s.Stmts {
		t := strings.TrimSuffix(strings.TrimSpace(st), ";")
		switch {
		case isNoopStmt(t):
		case isNotImplemented(t):
			notImpl = true
		default:
			return ""
		}
	}
	if notImpl {
		return stubNotImpl
	}
	if len(s.Comments) == 0 {
		return stubEmpty
	}
	for _, c := range s.Comments {
		if workMarker.MatchString(c) {
			return stubTodo
		}
	}
	return ""
}

var workMarker = regexp.MustCompile(`\b(TODO|FIXME|XXX|HACK)\b`)

// isNoopStmt reports a statement that does nothing: pass, an ellipsis, an
// empty statement, or a docstring.
func isNoopStmt(t string) bool {
	switch t {
	case "", "pass", "...", ";":
		return true
	}
	return strings.HasPrefix(t, `"""`) || strings.HasPrefix(t, `'''`)
}

var notImplemented = regexp.MustCompile(`(?i)^(?:panic\s*\(.*(?:not\s+(?:yet\s+)?implemented|unimplemented|todo)|raise\s+NotImplementedError\b|throw\b.*(?:not\s+(?:yet\s+)?implemented|unimplemented|NotImplemented|UnsupportedOperationException)|(?:unimplemented|todo)!\s*\()`)

// isNotImplemented reports a statement that only raises "not implemented".
func isNotImplemented(t string) bool {
	return notImplemented.MatchString(t)
}

// declSite is one class member declaration, for matching a method against
// the interface, abstract and base-class methods it implements.
type declSite struct {
	file, class   string
	hasBody       bool
	classHasBases bool
}

// declIndex holds every class member of the reviewed files by language and
// name. Go is left out: its interface implementations are type-checked
// (FunctionEdgeInfo.Implements).
type declIndex map[string][]declSite

func declKey(lang, name string) string { return lang + "\x00" + name }

func (d declIndex) add(file string, f *reviewFile) {
	if f == nil || f.lang == "go" {
		return
	}
	for _, s := range f.spans {
		if s.Class == "" {
			continue
		}
		k := declKey(f.lang, s.Name)
		d[k] = append(d[k], declSite{file: file, class: s.Class, hasBody: s.HasBody, classHasBases: s.ClassHasBases})
	}
}

// reach says how a function with no callers is reached anyway. reason is
// empty when nothing reaches it (likely dead code); declaration marks an
// abstract-style base method (it only raises "not implemented" and
// subclasses override it), which is no stub at all.
type reach struct {
	reason      string
	declaration bool
}

// reach decides whether a function nobody calls is reached implicitly:
//   - Go: init, main in package main, a TestXxx/BenchmarkXxx/FuzzXxx/ExampleXxx
//     in a _test.go file, a method satisfying an interface, a function in a
//     build-constrained file (another platform's variant), the exported API
//     of a library package;
//   - other languages: a constructor (Java/C++ constructors, Ruby
//     initialize, PHP __construct, JS/TS constructor), main, a test function
//     in a test file or with @Test, a method implementing an interface or
//     abstract method or overriding a base-class method (@Override, or the
//     same name declared by another class while its class has bases), an
//     exported JS/TS function.
func (f *reviewFile) reach(c FunctionEdgeInfo, s funcSpan, decls declIndex) reach {
	if f.lang == "go" {
		switch {
		case s.Class == "" && (c.Name == "init" || (c.Name == "main" && f.goMain)):
			return reach{reason: "program entry point"}
		case strings.HasSuffix(c.File, "_test.go") && goTestName(c.Name):
			return reach{reason: "test function, run by go test"}
		case c.Implements != "":
			return reach{reason: "reached through " + c.Implements}
		case f.constrained:
			return reach{reason: "in a build-constrained file, one platform's variant"}
		case s.Exported:
			return reach{reason: "exported API of a library package"}
		}
		return reach{}
	}
	if s.Constructor {
		return reach{reason: "constructor, called implicitly"}
	}
	if s.Class == "" && c.Name == "main" {
		return reach{reason: "program entry point"}
	}
	if hasAnnotation(s, "Test") || (isTestFilePath(c.File) && testName(c.Name)) {
		return reach{reason: "test function, run by the test runner"}
	}
	if s.Class != "" {
		implements, overrides, overridden := false, false, false
		for _, d := range decls[declKey(f.lang, s.Name)] {
			if d.class == s.Class && d.file == c.File {
				continue
			}
			switch {
			case !d.hasBody:
				implements = true
			case d.classHasBases:
				overridden = true
			}
			if d.hasBody {
				overrides = true
			}
		}
		if stubBody(s) == stubNotImpl && overridden && !s.ClassHasBases {
			return reach{declaration: true}
		}
		if implements && (s.ClassHasBases || hasAnnotation(s, "Override")) {
			return reach{reason: "implements an interface or abstract method"}
		}
		if hasAnnotation(s, "Override") || (overrides && s.ClassHasBases) {
			return reach{reason: "overrides a base-class method"}
		}
	}
	if s.Exported {
		return reach{reason: "exported API of a library package"}
	}
	return reach{}
}

func hasAnnotation(s funcSpan, name string) bool {
	for _, a := range s.Annotations {
		if a == name {
			return true
		}
	}
	return false
}

// goTestName reports a name go test runs: TestXxx, BenchmarkXxx, FuzzXxx,
// ExampleXxx (or the bare prefix).
func goTestName(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if name == p || (strings.HasPrefix(name, p) && !isLowerASCII(name[len(p)])) {
			return true
		}
	}
	return false
}

func isLowerASCII(b byte) bool { return b >= 'a' && b <= 'z' }

// testName reports a test function name in a test file of another
// language: test_x, testX, TestX, or a test/it/describe callback.
func testName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "test") || strings.HasSuffix(lower, "_test") ||
		name == "it" || name == "describe" || lower == "setup" || lower == "teardown"
}
