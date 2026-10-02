package hooks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/pathutil"
)

// maxSourceBytes caps how much of a hook or grimoire file is read.
const maxSourceBytes = 1 << 20

// SourceKind says where a hook source lives, which decides whether it is
// trusted without approval.
type SourceKind string

const (
	KindGlobal         SourceKind = "global"          // ~/.celeste/hooks.json
	KindGlobalGrimoire SourceKind = "global-grimoire" // ~/.celeste/grimoire.md (v1)
	KindRepo           SourceKind = "repo"            // .celeste/hooks.json in the workspace or an ancestor
	KindRepoGrimoire   SourceKind = "repo-grimoire"   // any other grimoire file (v1)
)

// Source is one file that defines hooks.
type Source struct {
	// Path is the absolute path where the file was found, symlinks not
	// resolved. It is the trust key, so an approval never transfers to a
	// different location.
	Path string
	// Root is the project root the hooks run in: the directory holding
	// .celeste/, or the grimoire's directory. "" for global sources, which
	// run in the workspace.
	Root  string
	Kind  SourceKind
	Hooks []Definition // normalized
	Hash  string       // Hash(Hooks); for KindRepoStreamRules, the hash of Rules
	// Rules is the "## Stream Rules" text of a KindRepoStreamRules source,
	// shown for review before it is trusted.
	Rules string
	// streamRules is a repo grimoire's "## Stream Rules" body, which
	// Discover and SourcesAt list as a KindRepoStreamRules source of its own.
	streamRules string
}

// Global reports whether the source is the user's own file under
// ~/.celeste, trusted without approval.
func (s Source) Global() bool { return s.Kind == KindGlobal || s.Kind == KindGlobalGrimoire }

func globalHooksPath(home string) string    { return filepath.Join(home, ".celeste", "hooks.json") }
func globalGrimoirePath(home string) string { return filepath.Join(home, ".celeste", "grimoire.md") }

// Discover finds every hook source for workspace, in run order:
// ~/.celeste/hooks.json, ~/.celeste/grimoire.md, .celeste/hooks.json files
// from the filesystem root down to workspace, then every other grimoire file
// in grimoire priority order. A file that fails to read or parse (including
// a symlinked repo file or one over 1 MiB) is skipped with a warning.
// Sources that define no hooks are left out.
func Discover(workspace, home string) ([]Source, []string, error) {
	if !filepath.IsAbs(home) {
		return nil, nil, fmt.Errorf("home must be an absolute path: %q", home)
	}
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return nil, nil, err
	}
	var sources []Source
	var warnings []string
	add := func(path, root string, kind SourceKind) {
		src, warns, err := readSource(path, root, kind)
		warnings = append(warnings, warns...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("hooks: skipping %s: %s", strconv.Quote(path), safeText(err.Error())))
			return
		}
		if len(src.Hooks) > 0 {
			sources = append(sources, src)
		}
		if src.streamRules != "" {
			sources = append(sources, StreamRulesSource(path, src.streamRules))
		}
	}

	globalJSON, globalGrim := globalHooksPath(home), globalGrimoirePath(home)
	if fileExists(globalJSON) {
		add(globalJSON, "", KindGlobal)
	}
	if fileExists(globalGrim) {
		add(globalGrim, "", KindGlobalGrimoire)
	}

	var dirs []string
	for dir := ws; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		p := filepath.Join(dirs[i], ".celeste", "hooks.json")
		if lexists(p) && !samePath(p, globalJSON) {
			add(p, dirs[i], KindRepo)
		}
	}

	grims, err := grimoire.Discover(ws)
	if err != nil {
		return nil, nil, err
	}
	for _, g := range grims {
		if !samePath(g.Path, globalGrim) {
			add(g.Path, grimoireRoot(g.Path), KindRepoGrimoire)
		}
	}
	return sources, warnings, nil
}

// SourcesAt returns what `celeste hooks trust` acts on: everything Discover
// finds when target is a directory, or the one file when it is a file
// Discover would load. Anything else is an error.
func SourcesAt(target, home string) ([]Source, []string, error) {
	if !filepath.IsAbs(home) {
		return nil, nil, fmt.Errorf("home must be an absolute path: %q", home)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return Discover(abs, home)
	}
	kind, root, ok := classifyFile(abs, home)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a hook file Celeste loads (.celeste/hooks.json, .grimoire, .grimoire.local, .celeste/grimoire/*.md, or the global ~/.celeste files)", abs)
	}
	src, warns, err := readSource(abs, root, kind)
	if err != nil {
		return nil, warns, err
	}
	var out []Source
	if len(src.Hooks) > 0 {
		out = append(out, src)
	}
	if src.streamRules != "" {
		out = append(out, StreamRulesSource(abs, src.streamRules))
	}
	return out, warns, nil
}

func classifyFile(p, home string) (SourceKind, string, bool) {
	dir, base := filepath.Dir(p), filepath.Base(p)
	switch {
	case samePath(p, globalHooksPath(home)):
		return KindGlobal, "", true
	case samePath(p, globalGrimoirePath(home)):
		return KindGlobalGrimoire, "", true
	case base == "hooks.json" && filepath.Base(dir) == ".celeste":
		return KindRepo, filepath.Dir(dir), true
	case base == ".grimoire" || base == ".grimoire.local":
		return KindRepoGrimoire, dir, true
	case filepath.Ext(base) == ".md" && filepath.Base(dir) == "grimoire" && filepath.Base(filepath.Dir(dir)) == ".celeste":
		return KindRepoGrimoire, filepath.Dir(filepath.Dir(dir)), true
	}
	return "", "", false
}

// grimoireRoot is the project root for a grimoire file:
// <root>/.celeste/grimoire/x.md -> <root>; otherwise the file's directory.
func grimoireRoot(p string) string {
	dir := filepath.Dir(p)
	if filepath.Base(dir) == "grimoire" && filepath.Base(filepath.Dir(dir)) == ".celeste" {
		return filepath.Dir(filepath.Dir(dir))
	}
	return dir
}

func readSource(path, root string, kind SourceKind) (Source, []string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Source{}, nil, err
	}
	global := kind == KindGlobal || kind == KindGlobalGrimoire
	if info.Mode()&os.ModeSymlink != 0 && !global {
		return Source{}, nil, errors.New("refusing a symlinked repo hook file; copy the file instead")
	}
	regularInfo := info
	if global {
		regularInfo, err = os.Stat(path)
		if err != nil {
			return Source{}, nil, err
		}
	}
	if !regularInfo.Mode().IsRegular() {
		return Source{}, nil, errors.New("not a regular file")
	}
	if !global && root != "" {
		if err := refuseSymlinkedRepoComponents(path, root); err != nil {
			return Source{}, nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return Source{}, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSourceBytes+1))
	if err != nil {
		return Source{}, nil, err
	}
	if len(data) > maxSourceBytes {
		return Source{}, nil, errors.New("file is larger than 1 MiB")
	}

	src := Source{Path: path, Root: root, Kind: kind}
	var warnings []string
	switch kind {
	case KindGlobal, KindRepo:
		defs, err := ParseFile(data)
		if err != nil {
			return Source{}, nil, err
		}
		src.Hooks = defs
	default:
		g, err := grimoire.Parse(string(data), filepath.Dir(path))
		if err != nil {
			return Source{}, nil, err
		}
		defs, skipped := FromGrimoire(g.Hooks)
		for _, s := range skipped {
			warnings = append(warnings, fmt.Sprintf("hooks: %s: ignoring v1 hook %s", strconv.Quote(path), s))
		}
		if len(defs) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"hooks: %s uses a grimoire \"## Hooks\" section (protocol v1); move it to a hooks.json file (see docs/HOOKS.md)", strconv.Quote(path)))
		}
		src.Hooks = defs
		if kind == KindRepoGrimoire {
			// One "## Stream Rules" per file (grimoire.Parse keeps the
			// last of a repeated heading). The global grimoire's are the
			// user's own and need no trust.
			var bodies []string
			for _, r := range g.StreamRules {
				bodies = append(bodies, r.Body)
			}
			src.streamRules = strings.TrimSpace(strings.Join(bodies, "\n"))
		}
	}
	src.Hash = Hash(src.Hooks)
	return src, warnings, nil
}

func refuseSymlinkedRepoComponents(path, root string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(rel)
	if dir == "." {
		return nil
	}
	cur := root
	for _, part := range strings.Split(dir, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		// Windows junctions report ModeIrregular instead of ModeSymlink on Go 1.23+.
		if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("refusing a symlinked repo hook file; copy the file instead")
		}
	}
	return nil
}

// samePath is pathutil.Same. It is used only to recognise the global
// files, never as a trust key.
func samePath(a, b string) bool { return pathutil.Same(a, b) }

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// lexists is fileExists that also sees symlinks (even dangling ones), so a
// symlinked repo file is reported, not silently ignored.
func lexists(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && !info.IsDir()
}
