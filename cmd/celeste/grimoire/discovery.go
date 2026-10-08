package grimoire

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"

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
	st := newIncludeState()
	for _, src := range sources {
		data, err := readSource(src)
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
		// grimoire may reach outside the repository (see includeScope).
		scope := repo
		if src.dir == "" {
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

// readSource reads one grimoire source, at most MaxSize bytes of it. A
// source found in the workspace is repository content: it must be a
// regular file, not a symlink, in a directory that resolves inside the
// one it was found in, so a cloned repository cannot point it at a file
// elsewhere. The global ~/.celeste/grimoire.md may be a symlink.
func readSource(src GrimoireSource) (string, error) {
	path := src.Path
	if src.dir == "" {
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
