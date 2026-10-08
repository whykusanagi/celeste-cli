package grimoire

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
)

// Priority levels for grimoire sources (lowest to highest).
const (
	PriorityGlobal   = 10 // ~/.celeste/grimoire.md
	PriorityProject  = 20 // /repo-root/.grimoire
	PriorityFragment = 30 // /repo-root/.celeste/grimoire/*.md
	PriorityLocal    = 40 // /repo-root/.grimoire.local
)

// GrimoireSource represents a discovered .grimoire file with its priority.
type GrimoireSource struct {
	Path     string
	Priority int
	// dir is the directory the source was found in; "" for the global one.
	dir string
}

// Discover walks upward from startDir to the filesystem root, collecting
// all .grimoire sources ordered by priority (lowest first).
func Discover(startDir string) ([]GrimoireSource, error) {
	startDir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	var sources []GrimoireSource

	// Check global grimoire (~/.celeste/grimoire.md)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(homeDir, ".celeste", "grimoire.md")
		if fileExists(globalPath) {
			sources = append(sources, GrimoireSource{
				Path:     globalPath,
				Priority: PriorityGlobal,
			})
		}
	}

	// Walk upward from startDir to root, collecting sources.
	// We collect directories from startDir upward, then process them
	// from root downward so that closer-to-cwd dirs get higher priority.
	var dirs []string
	dir := startDir
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	// Process from root (end of slice) to startDir (beginning),
	// giving closer directories higher effective priority via depth multiplier.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		depth := len(dirs) - i // 1 for root, higher for closer dirs
		sources = append(sources, discoverAtDir(d, depth)...)
	}

	// Sort by priority (stable, lowest first)
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].Priority < sources[j].Priority
	})

	return sources, nil
}

// discoverAtDir checks a single directory for grimoire sources.
func discoverAtDir(dir string, depth int) []GrimoireSource {
	var sources []GrimoireSource
	depthFactor := depth * 100 // higher depth = closer to cwd = higher priority

	// Check .grimoire file
	grimPath := filepath.Join(dir, ".grimoire")
	if fileExists(grimPath) {
		sources = append(sources, GrimoireSource{
			Path:     grimPath,
			Priority: PriorityProject + depthFactor,
			dir:      dir,
		})
	}

	// Check .celeste/grimoire/ directory for *.md fragments
	fragDir := filepath.Join(dir, ".celeste", "grimoire")
	if dirExists(fragDir) {
		matches, err := filepath.Glob(filepath.Join(fragDir, "*.md"))
		if err == nil {
			sort.Strings(matches) // deterministic order
			for _, m := range matches {
				sources = append(sources, GrimoireSource{
					Path:     m,
					Priority: PriorityFragment + depthFactor,
					dir:      dir,
				})
			}
		}
	}

	// Check .grimoire.local file
	localPath := filepath.Join(dir, ".grimoire.local")
	if fileExists(localPath) {
		sources = append(sources, GrimoireSource{
			Path:     localPath,
			Priority: PriorityLocal + depthFactor,
			dir:      dir,
		})
	}

	return sources
}

// LoadAll discovers, parses, and merges all grimoire sources for the given directory.
func LoadAll(startDir string) (*Grimoire, error) {
	sources, err := Discover(startDir)
	if err != nil {
		return &Grimoire{RawSections: make(map[string]string)}, err
	}
	if len(sources) == 0 {
		return &Grimoire{RawSections: make(map[string]string)}, nil
	}

	var grimoires []*Grimoire
	repo := repoScope(startDir)
	owned := userOwnedDirs(startDir)
	st := newIncludeState()
	defer st.close()
	for _, src := range sources {
		userOwned := src.dir == "" || owned(src.dir)
		data, err := readSource(src, userOwned)
		if err != nil {
			log.Printf("grimoire: skipping %s: %v", src.Path, err)
			continue
		}
		g, err := Parse(data, filepath.Dir(src.Path))
		if err != nil {
			log.Printf("grimoire: parse error in %s: %v", src.Path, err)
			continue
		}
		g.Sources = []string{src.Path}
		for i := range g.StreamRules {
			g.StreamRules[i].Source = src.Path
		}
		// Includes resolve against startDir; only the user's own
		// grimoires may reach outside the repository (see includeScope).
		scope := repo
		if userOwned {
			scope = includeScope{global: true}
		}
		resolveIncludesIn(g, startDir, scope, st)
		grimoires = append(grimoires, g)
	}

	if len(grimoires) == 0 {
		return &Grimoire{RawSections: make(map[string]string)}, nil
	}

	return Merge(grimoires...), nil
}

// userOwnedDirs reports which directories of the upward walk from
// startDir hold the user's own grimoires rather than repository content:
// the home directory and its ancestors, and, inside a git repository, a
// directory above its root that sits right under home
// (~/Development/.grimoire, say). Any other directory may be an extracted
// archive, a nested .git inside it included (CodeRabbit review of #421);
// outside a repository only home and its ancestors are the user's.
func userOwnedDirs(startDir string) func(dir string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	var home string
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		home = resolve(h)
	}
	var gitRoot string
	if abs, err := filepath.Abs(startDir); err == nil {
		if r, ok := GitRoot(abs); ok {
			gitRoot = resolve(r)
		}
	}
	return func(dir string) bool {
		d := resolve(dir)
		if home != "" && pathutil.Within(d, home) {
			return true // home itself or one of its ancestors
		}
		return home != "" && gitRoot != "" && !pathutil.Within(gitRoot, d) &&
			filepath.Dir(d) == home
	}
}

// readSource reads one grimoire source, at most MaxSize bytes of it, cut
// on a rune boundary. A source in the repository is repository content:
// it must be a regular file, not a symlink, in a directory that resolves
// inside the one it was found in, so a cloned repository cannot point it
// at a file elsewhere. The user's own grimoires (userOwned: the global
// ~/.celeste/grimoire.md and those userOwnedDirs names) may be symlinks.
func readSource(src GrimoireSource, userOwned bool) (string, error) {
	path := src.Path
	if userOwned {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		path = real
	} else {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("not a regular file (repository grimoires may not be symlinks)")
		}
		realDir, err := filepath.EvalSymlinks(src.dir)
		if err != nil {
			return "", err
		}
		realParent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		if !pathutil.Within(realDir, realParent) {
			return "", fmt.Errorf("its directory links outside %s", src.dir)
		}
	}
	f, _, err := openRegular(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxSize {
		log.Printf("grimoire: %s is over %d KB; reading the first %d KB", src.Path, MaxSize/1024, MaxSize/1024)
		data = data[:MaxSize]
		// The cut may split the last rune: drop its leading bytes.
		for i := 0; i < utf8.UTFMax-1 && len(data) > 0; i++ {
			if r, n := utf8.DecodeLastRune(data); r != utf8.RuneError || n != 1 {
				break
			}
			data = data[:len(data)-1]
		}
	}
	return string(data), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
