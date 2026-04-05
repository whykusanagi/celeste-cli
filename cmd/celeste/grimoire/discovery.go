package grimoire

import (
	"log"
	"os"
	"path/filepath"
	"sort"
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
	for _, src := range sources {
		data, err := os.ReadFile(src.Path)
		if err != nil {
			log.Printf("grimoire: skipping %s: %v", src.Path, err)
			continue
		}
		g, err := Parse(string(data), filepath.Dir(src.Path))
		if err != nil {
			log.Printf("grimoire: parse error in %s: %v", src.Path, err)
			continue
		}
		g.Sources = []string{src.Path}
		grimoires = append(grimoires, g)
	}

	if len(grimoires) == 0 {
		return &Grimoire{RawSections: make(map[string]string)}, nil
	}

	result := Merge(grimoires...)

	// Resolve includes using the startDir as base
	if err := ResolveIncludes(result, startDir); err != nil {
		// Non-fatal: include resolution errors are recorded on individual refs
		_ = err
	}

	return result, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
