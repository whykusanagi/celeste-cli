package codegraph

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// goload.go loads the workspace's Go packages and type-checks them with
// go/types, standard library only. Workspace packages are found from the
// files the indexer walked; their import paths come from the nearest
// go.mod (or a synthetic "_/<dir>" path when there is none). Imports from
// outside the workspace (the standard library and module dependencies) are
// type-checked from source without function bodies. Their directories come
// from one `go list -deps` per module when the go command is available, and
// from go/build (GOROOT only, no go command) otherwise.

// goListTimeout bounds the single `go list` run per module.
const goListTimeout = 2 * time.Minute

// goUnit is one type-checked package. Each directory yields up to three,
// mirroring how the go command builds tests:
//   - the package itself (non-test files): what other packages import;
//   - the test-augmented package (the same files plus in-package _test.go
//     files), used only for the test files' edges, so test-only imports
//     never take part in the import graph;
//   - the external _test package, which imports the test-augmented one.
type goUnit struct {
	dir   string // absolute
	path  string // import path
	name  string
	files []*goFile
	// aug is the test-augmented unit an external test package imports in
	// place of path (nil when there are no in-package tests).
	aug *goUnit

	pkg   *types.Package
	info  *types.Info
	errs  int
	state int // 0 unchecked, 1 checking, 2 done
}

// goFile is one parsed workspace Go file.
type goFile struct {
	rel  string // workspace-relative, as the indexer's walker produced it
	ast  *ast.File
	unit *goUnit // nil when the file falls back to the AST heuristic
}

// goLoader holds everything one Go pass loads.
type goLoader struct {
	ctx       context.Context
	workspace string
	fset      *token.FileSet
	bctx      build.Context

	units  map[string]*goUnit // importable units by import path
	tunits []*goUnit          // test-augmented units
	xunits []*goUnit          // external _test packages
	files  []*goFile

	listed     map[string]listedPkg         // go list results by import path
	importMaps map[string]map[string]string // dir -> vendor import map
	ext        map[string]*extPkg
}

type listedPkg struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	ImportMap  map[string]string
}

type extPkg struct {
	pkg   *types.Package
	err   error
	state int
}

// loadGo parses relFiles (workspace-relative Go files) and type-checks every
// package they form. Files that cannot join a package (build constraints,
// mismatched package clause) are returned with a nil unit; files that do
// not parse are dropped (the heuristic parser cannot read them either).
func loadGo(ctx context.Context, workspace string, relFiles []string) (*goLoader, error) {
	l := &goLoader{
		ctx:        ctx,
		workspace:  workspace,
		fset:       token.NewFileSet(),
		bctx:       build.Default,
		units:      map[string]*goUnit{},
		listed:     map[string]listedPkg{},
		importMaps: map[string]map[string]string{},
		ext:        map[string]*extPkg{},
	}
	// go/build shells out to `go list` for module imports unless a file
	// system hook is set; setting one keeps the fallback in-process (GOROOT
	// and vendor lookups only). Module dependencies come from listModule.
	l.bctx.JoinPath = filepath.Join
	// Analyse the CGO_ENABLED=0 build, so results do not depend on
	// whether the host has a C compiler: cgo-tagged files and files
	// importing "C" fall back to the heuristic.
	l.bctx.CgoEnabled = false

	// Group by slash-separated directory; keep each file's path exactly as
	// the walker produced it (OS separators), since that is the key the
	// symbols and file records are stored under.
	byDir := map[string][]string{}
	for _, rel := range relFiles {
		dir := path.Dir(filepath.ToSlash(rel))
		byDir[dir] = append(byDir[dir], rel)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	modRoots := map[string]bool{}
	modCache := map[string][2]string{}
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		absDir := filepath.Join(workspace, filepath.FromSlash(d))
		modRoot, modPath := findModule(absDir, modCache)
		importPath := "_/" + d
		if d == "." {
			importPath = "_"
		}
		if modRoot != "" {
			modRoots[modRoot] = true
			rel, err := filepath.Rel(modRoot, absDir)
			if err == nil && rel == "." {
				importPath = modPath
			} else if err == nil {
				importPath = modPath + "/" + filepath.ToSlash(rel)
			}
		}
		l.loadDir(absDir, importPath, byDir[d])
	}

	if goBin, err := exec.LookPath("go"); err == nil {
		if l.bctx.GOROOT == "" {
			// Release binaries are built with -trimpath, so they do not
			// know a GOROOT; ask the go command for the standard library.
			l.bctx.GOROOT = goEnvGOROOT(ctx, goBin)
		}
		roots := make([]string, 0, len(modRoots))
		for r := range modRoots {
			roots = append(roots, r)
		}
		sort.Strings(roots)
		for _, r := range roots {
			l.listModule(goBin, r)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(l.units))
	for p := range l.units {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.check(l.units[p])
	}
	for _, u := range append(append([]*goUnit{}, l.tunits...), l.xunits...) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.check(u)
	}
	return l, ctx.Err()
}

// loadDir parses one directory's files and groups them into units.
func (l *goLoader) loadDir(absDir, importPath string, rels []string) {
	sort.Strings(rels)
	type parsed struct {
		f    *goFile
		test bool
	}
	var all []parsed
	names := map[string]int{}
	for _, rel := range rels {
		abs := filepath.Join(l.workspace, rel)
		// Read through the workspace root: a file replaced by a symlink
		// or a FIFO since the walk is skipped (Aikido review of #421).
		src, err := readConfined(l.workspace, rel)
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(l.fset, abs, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil || f == nil {
			continue
		}
		gf := &goFile{rel: rel, ast: f}
		l.files = append(l.files, gf)
		match, err := l.bctx.MatchFile(absDir, filepath.Base(abs))
		if err != nil || !match || importsC(f) {
			continue // excluded by build constraints or cgo: heuristic fallback
		}
		test := strings.HasSuffix(rel, "_test.go")
		all = append(all, parsed{gf, test})
		if !test {
			names[f.Name.Name]++
		}
	}
	if len(all) == 0 {
		return
	}
	// The package name is the most common name among non-test files; a
	// directory of only tests uses its non-_test package name.
	base := ""
	best := 0
	for n, c := range names {
		if c > best || (c == best && n < base) {
			base, best = n, c
		}
	}
	if base == "" {
		for _, p := range all {
			if n := p.f.ast.Name.Name; !strings.HasSuffix(n, "_test") {
				base = n
				break
			}
		}
		if base == "" {
			base = strings.TrimSuffix(all[0].f.ast.Name.Name, "_test")
		}
	}
	unit := &goUnit{dir: absDir, path: importPath, name: base}
	aug := &goUnit{dir: absDir, path: importPath, name: base}
	var xunit *goUnit
	for _, p := range all {
		n := p.f.ast.Name.Name
		switch {
		case n == base && !p.test:
			unit.files = append(unit.files, p.f)
			aug.files = append(aug.files, p.f)
			p.f.unit = unit
		case n == base:
			aug.files = append(aug.files, p.f)
			p.f.unit = aug
		case p.test && n == base+"_test":
			if xunit == nil {
				xunit = &goUnit{dir: absDir, path: importPath + "_test", name: n}
			}
			xunit.files = append(xunit.files, p.f)
			p.f.unit = xunit
		}
	}
	if len(unit.files) > 0 {
		l.units[importPath] = unit
	}
	if len(aug.files) > len(unit.files) {
		l.tunits = append(l.tunits, aug)
	} else {
		aug = nil
	}
	if xunit != nil {
		xunit.aug = aug
		l.xunits = append(l.xunits, xunit)
	}
}

// check type-checks a workspace unit with function bodies.
func (l *goLoader) check(u *goUnit) {
	if u.state != 0 {
		return
	}
	u.state = 1
	u.info = &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	conf := types.Config{
		Importer: &goImporter{l: l, self: u.aug},
		Sizes:    types.SizesFor("gc", l.bctx.GOARCH),
		Error:    func(error) { u.errs++ },
	}
	asts := make([]*ast.File, len(u.files))
	for i, f := range u.files {
		asts[i] = f.ast
	}
	pkg, _ := conf.Check(u.path, l.fset, asts, u.info)
	u.pkg = pkg
	u.state = 2
}

// goImporter resolves imports for both workspace and external packages.
// self, when set, is the test-augmented unit an external test package
// sees under its package's import path.
type goImporter struct {
	l    *goLoader
	self *goUnit
}

func (g *goImporter) Import(p string) (*types.Package, error) {
	return g.ImportFrom(p, "", 0)
}

func (g *goImporter) ImportFrom(p, dir string, _ types.ImportMode) (*types.Package, error) {
	l := g.l
	if err := l.ctx.Err(); err != nil {
		return nil, err
	}
	if p == "unsafe" {
		return types.Unsafe, nil
	}
	if m := l.importMaps[dir]; m != nil && m[p] != "" {
		p = m[p]
	}
	u := l.units[p]
	if g.self != nil && g.self.path == p {
		u = g.self
	}
	if u != nil {
		switch u.state {
		case 1:
			return nil, fmt.Errorf("import cycle through %s", p)
		case 0:
			l.check(u)
		}
		if u.pkg == nil {
			return nil, fmt.Errorf("package %s did not type-check", p)
		}
		return u.pkg, nil
	}
	return l.importExternal(p, dir)
}

// importExternal type-checks a non-workspace package from source, without
// function bodies, tolerating errors so one bad dependency degrades only
// the expressions that use it.
func (l *goLoader) importExternal(p, fromDir string) (*types.Package, error) {
	if e := l.ext[p]; e != nil {
		if e.state == 1 {
			return nil, fmt.Errorf("import cycle through %s", p)
		}
		return e.pkg, e.err
	}
	e := &extPkg{state: 1}
	l.ext[p] = e
	defer func() { e.state = 2 }()

	var dir string
	var names []string
	if lp, ok := l.listed[p]; ok && lp.Dir != "" {
		dir = lp.Dir
		names = lp.GoFiles
	} else {
		bp, err := l.bctx.Import(p, fromDir, 0)
		if err != nil && bp == nil {
			e.err = err
			return nil, err
		}
		if bp == nil || len(bp.GoFiles) == 0 {
			e.err = fmt.Errorf("cannot find package %s", p)
			if err != nil {
				e.err = err
			}
			return nil, e.err
		}
		dir = bp.Dir
		names = bp.GoFiles
	}
	key := l.stdKey(dir)
	if pkg := stdPkgs.get(key); pkg != nil {
		e.pkg = pkg
		return pkg, nil
	}
	var asts []*ast.File
	for _, n := range names {
		f, err := parser.ParseFile(l.fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err == nil && f != nil {
			asts = append(asts, f)
		}
	}
	if len(asts) == 0 {
		e.err = fmt.Errorf("no parsable files in %s", p)
		return nil, e.err
	}
	conf := types.Config{
		Importer:         &goImporter{l: l},
		IgnoreFuncBodies: true,
		Sizes:            types.SizesFor("gc", l.bctx.GOARCH),
		Error:            func(error) {},
	}
	pkg, _ := conf.Check(p, l.fset, asts, nil)
	e.pkg = pkg
	if pkg == nil {
		e.err = fmt.Errorf("type-check %s failed", p)
	} else {
		stdPkgs.put(key, pkg)
	}
	return e.pkg, e.err
}

// stdPkgs keeps type-checked standard-library packages for the life of the
// process. They never change for one GOROOT, GOOS and GOARCH, and checking
// them from source is most of an index run's time (and pushed the codegraph
// tests past 20 minutes under -race on Windows). Only exported types are
// looked up in them, never their positions, so sharing one across loaders
// with different FileSets is safe.
// ponytail: unbounded, a few hundred packages at most; evict if memory matters.
var stdPkgs = &pkgCache{m: map[string]*types.Package{}}

type pkgCache struct {
	mu sync.Mutex
	m  map[string]*types.Package
}

func (c *pkgCache) get(key string) *types.Package {
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key]
}

func (c *pkgCache) put(key string, pkg *types.Package) {
	if key == "" {
		return
	}
	c.mu.Lock()
	c.m[key] = pkg
	c.mu.Unlock()
}

// stdKey is the cache key for a package directory inside GOROOT/src, or ""
// for any other package (module code is not shared: it can change).
func (l *goLoader) stdKey(dir string) string {
	if l.bctx.GOROOT == "" {
		return ""
	}
	src := filepath.Join(l.bctx.GOROOT, "src") + string(filepath.Separator)
	if !strings.HasPrefix(dir+string(filepath.Separator), src) {
		return ""
	}
	return l.bctx.GOOS + "/" + l.bctx.GOARCH + "|" + dir
}

// listModule records the directories and files of every package the module
// at root depends on (including test dependencies) with one `go list` run.
// It never touches the network (GOPROXY=off) or switches toolchains
// (GOTOOLCHAIN=local); a failure just leaves those imports to go/build.
func (l *goLoader) listModule(goBin, root string) {
	ctx, cancel := context.WithTimeout(l.ctx, goListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "list", goListModFlag(root), "-e", "-deps", "-test",
		"-json=ImportPath,Dir,GoFiles,ImportMap", "./...")
	cmd.Dir = root
	// os/exec keeps the last value of a duplicated variable, so these
	// overrides win over the inherited environment.
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	_ = cmd.Run() // -e: partial output is still useful on a non-zero exit
	dec := json.NewDecoder(bufio.NewReader(&stdout))
	for {
		var lp listedPkg
		if err := dec.Decode(&lp); err != nil {
			if !errors.Is(err, io.EOF) {
				return
			}
			break
		}
		// Skip test variants ("p [p.test]") and generated test mains.
		if strings.Contains(lp.ImportPath, " ") || strings.HasSuffix(lp.ImportPath, ".test") {
			continue
		}
		if _, seen := l.listed[lp.ImportPath]; !seen {
			l.listed[lp.ImportPath] = lp
		}
		if len(lp.ImportMap) > 0 && lp.Dir != "" {
			l.importMaps[lp.Dir] = lp.ImportMap
		}
	}
}

// goListModFlag is the -mod flag for `go list` in the module at root. It is
// always explicit so a GOFLAGS=-mod=mod in the user's environment cannot
// rewrite the indexed module's go.mod/go.sum: vendor when the module is
// vendored, readonly otherwise.
func goListModFlag(root string) string {
	if _, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); err == nil {
		return "-mod=vendor"
	}
	return "-mod=readonly"
}

// goEnvGOROOT returns `go env GOROOT`, or "" on any failure.
func goEnvGOROOT(ctx context.Context, goBin string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "env", "GOROOT")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// importsC reports whether a file uses cgo.
func importsC(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return true
		}
	}
	return false
}

// goModFingerprint hashes the go.mod and go.sum of every module the given
// Go files belong to. Update compares it with the stored value so a module
// rename or a dependency change re-runs the Go pass even when no .go file
// changed.
func goModFingerprint(workspace string, relFiles []string) string {
	cache := map[string][2]string{}
	roots := map[string]bool{}
	seenDir := map[string]bool{}
	for _, rel := range relFiles {
		dir := filepath.Dir(filepath.Join(workspace, rel))
		if seenDir[dir] {
			continue
		}
		seenDir[dir] = true
		if root, _ := findModule(dir, cache); root != "" {
			roots[root] = true
		}
	}
	sorted := make([]string, 0, len(roots))
	for r := range roots {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)
	h := sha256.New()
	for _, r := range sorted {
		h.Write([]byte(r + "\x00"))
		for _, name := range []string{"go.mod", "go.sum"} {
			data, _ := os.ReadFile(filepath.Join(r, name))
			h.Write([]byte(name + "\x00"))
			h.Write(data)
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// findModule walks up from dir to the nearest go.mod and returns its
// directory and module path ("", "" when there is none).
func findModule(dir string, cache map[string][2]string) (string, string) {
	var visited []string
	d := dir
	for {
		if r, ok := cache[d]; ok {
			for _, v := range visited {
				cache[v] = r
			}
			return r[0], r[1]
		}
		visited = append(visited, d)
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			if mp := modulePath(data); mp != "" {
				r := [2]string{d, mp}
				for _, v := range visited {
					cache[v] = r
				}
				return d, mp
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			for _, v := range visited {
				cache[v] = [2]string{}
			}
			return "", ""
		}
		d = parent
	}
}

// modulePath extracts the module path from go.mod contents.
func modulePath(mod []byte) string {
	for _, line := range strings.Split(string(mod), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "module"))
		if i := strings.Index(rest, "//"); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		return strings.Trim(rest, "\"`")
	}
	return ""
}
