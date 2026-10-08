package grimoire

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gitsafe"
)

// ProjectInfo holds detected project metadata.
type ProjectInfo struct {
	Language     string
	ModulePath   string
	TestCommand  string
	LintCommand  string
	BuildCommand string
	EntryPoint   string
	Framework    string
}

// DetectProject inspects the given directory and returns project information.
func DetectProject(dir string) (*ProjectInfo, error) {
	info := &ProjectInfo{Language: "unknown"}

	// Detect all present languages by manifest files AND file count.
	// For multi-language projects, the dominant language (most source files) wins.
	type langCandidate struct {
		language    string
		testCmd     string
		lintCmd     string
		buildCmd    string
		modulePath  string
		hasManifest bool
		fileCount   int
	}
	candidates := make(map[string]*langCandidate)

	// Check for Go project
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		c := &langCandidate{language: "go", testCmd: "go test ./... -count=1", lintCmd: "golangci-lint run ./...", buildCmd: "go build ./...", hasManifest: true}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "module ") {
				c.modulePath = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module"))
				break
			}
		}
		candidates["go"] = c
	}

	// Check for Node.js/TypeScript project
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		lang := "javascript"
		if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err == nil {
			lang = "typescript"
		}
		// npm run build fails without a build script: named only with one.
		c := &langCandidate{language: lang, testCmd: "npm test", hasManifest: true}
		var pkg map[string]any
		if err := json.Unmarshal(data, &pkg); err == nil {
			if name, ok := pkg["name"].(string); ok {
				c.modulePath = name
			}
			if scripts, ok := pkg["scripts"].(map[string]any); ok {
				if test, ok := scripts["test"].(string); ok {
					c.testCmd = test
				}
				if lint, ok := scripts["lint"].(string); ok {
					c.lintCmd = lint
				}
				if _, ok := scripts["build"].(string); ok {
					c.buildCmd = "npm run build"
				}
			}
		}
		candidates[lang] = c
	}

	// Check for Python project
	if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		candidates["python"] = &langCandidate{language: "python", testCmd: "pytest", lintCmd: "ruff check .", hasManifest: true}
	} else if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		candidates["python"] = &langCandidate{language: "python", testCmd: "pytest", lintCmd: "ruff check .", hasManifest: true}
	}

	// Check for Rust project
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		candidates["rust"] = &langCandidate{language: "rust", testCmd: "cargo test", lintCmd: "cargo clippy", buildCmd: "cargo build", hasManifest: true}
	}

	// If multiple languages detected, count source files to find the dominant one
	if len(candidates) > 1 {
		extToLang := map[string]string{
			".go": "go", ".py": "python", ".js": "javascript",
			".ts": "typescript", ".tsx": "typescript", ".rs": "rust",
		}
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				name := d.Name()
				if d.IsDir() && (name == "node_modules" || name == "venv" || name == ".venv" || name == ".git" || name == "__pycache__" || name == "vendor" || name == "target") {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(d.Name())
			if lang, ok := extToLang[ext]; ok {
				if c, ok := candidates[lang]; ok {
					c.fileCount++
				}
			}
			return nil
		})
	}

	// Pick the winner — most source files, or single candidate
	var winner *langCandidate
	for _, c := range candidates {
		if winner == nil || c.fileCount > winner.fileCount {
			winner = c
		}
	}

	if winner != nil {
		info.Language = winner.language
		info.TestCommand = winner.testCmd
		info.LintCommand = winner.lintCmd
		info.BuildCommand = winner.buildCmd
		info.ModulePath = winner.modulePath
		return info, nil
	}

	return info, nil
}

// GrimoireMeta returns a metadata header block for the grimoire file.
// Includes timestamp, git hash, and branch so staleness can be detected.
func GrimoireMeta(dir string) string {
	var sb strings.Builder
	sb.WriteString("<!--\n")
	sb.WriteString(fmt.Sprintf("last_updated: %s\n", time.Now().Format("2006-01-02 15:04:05")))

	// Git info
	if hash := gitCommand(dir, "rev-parse", "--short", "HEAD"); hash != "" {
		sb.WriteString(fmt.Sprintf("git_hash: %s\n", hash))
	}
	if branch := gitCommand(dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		sb.WriteString(fmt.Sprintf("git_branch: %s\n", branch))
	}
	if count := gitCommand(dir, "rev-list", "--count", "HEAD"); count != "" {
		sb.WriteString(fmt.Sprintf("git_commit_count: %s\n", count))
	}

	sb.WriteString("-->\n")
	return sb.String()
}

// gitCommand runs a git command and returns trimmed stdout, or "" on error.
func gitCommand(dir string, args ...string) string {
	cmd, err := gitsafe.Command(context.Background(), dir, args...)
	if err != nil {
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GenerateTemplate creates a starter .grimoire file from project info.
func GenerateTemplate(info *ProjectInfo, dir string) string {
	var sb strings.Builder

	// Metadata header (HTML comment — invisible in rendered markdown)
	sb.WriteString(GrimoireMeta(dir))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("# Grimoire: %s project\n\n", info.Language))

	// Bindings
	sb.WriteString("## Bindings\n")
	sb.WriteString(fmt.Sprintf("- This is a %s project\n", info.Language))
	if info.ModulePath != "" {
		switch info.Language {
		case "go":
			sb.WriteString(fmt.Sprintf("- Module path: %s\n", info.ModulePath))
		default:
			sb.WriteString(fmt.Sprintf("- Package name: %s\n", info.ModulePath))
		}
	}
	if info.Language == "go" {
		sb.WriteString("- Use standard library conventions\n")
	}
	sb.WriteString("\n")

	// Rituals
	sb.WriteString("## Rituals\n")
	if info.TestCommand != "" {
		sb.WriteString(fmt.Sprintf("- Always run tests before committing: `%s`\n", info.TestCommand))
	}
	if info.LintCommand != "" {
		sb.WriteString(fmt.Sprintf("- Run linter before commits: `%s`\n", info.LintCommand))
	}
	sb.WriteString("- Use conventional commit messages\n")
	sb.WriteString("\n")

	// Incantations
	sb.WriteString("## Incantations\n")
	sb.WriteString("# Add file references to include as context:\n")
	sb.WriteString("# @./docs/ARCHITECTURE.md\n")
	sb.WriteString("# @./docs/STYLE_GUIDE.md\n")
	sb.WriteString("\n")

	// Wards
	sb.WriteString("## Wards\n")
	sb.WriteString("- Do not modify .celeste/ directory contents\n")
	sb.WriteString("# Add paths that should not be modified without explicit permission:\n")
	sb.WriteString("# - Do not modify .env or secrets files\n")
	sb.WriteString("\n")

	return sb.String()
}

// Init creates a .grimoire file in the given directory. It writes nothing
// else: the project's .gitignore is left alone (2.0 W4). Returns the path to
// the created file, or an error if one already exists.
func Init(dir string) (string, error) {
	grimPath := filepath.Join(dir, ".grimoire")

	// Check if .grimoire already exists
	if _, err := os.Lstat(grimPath); err == nil {
		return "", existsError{".grimoire", grimPath}
	}

	info, err := DetectProject(dir)
	if err != nil {
		return "", fmt.Errorf("project detection failed: %w", err)
	}

	if err := writeNew(grimPath, GenerateTemplate(info, dir)); err != nil {
		if os.IsExist(err) {
			return "", existsError{".grimoire", grimPath}
		}
		return "", fmt.Errorf("failed to write .grimoire: %w", err)
	}

	return grimPath, nil
}

// HasProjectContext reports a project grimoire (.grimoire, .grimoire.local,
// .celeste/grimoire/*.md) that LoadAll would load, or a context file
// (AGENTS.md, CLAUDE.md) between the git root and the workspace (2.0 W4,
// ruling 6). The global ~/.celeste/grimoire.md is not project context, and
// neither is an empty or blank grimoire, which renders nothing.
func HasProjectContext(workspace string) bool {
	sources, _ := Discover(workspace)
	for _, src := range sources {
		if src.Priority == PriorityGlobal {
			continue
		}
		if data, err := os.ReadFile(src.Path); err == nil && strings.TrimSpace(string(data)) != "" {
			return true
		}
	}
	files, _ := ContextFiles(workspace)
	return len(files) > 0
}

// AgentsTemplate is a starting AGENTS.md for the detected project.
func AgentsTemplate(info *ProjectInfo) string {
	var b strings.Builder
	b.WriteString("# AGENTS.md\n\nInstructions for coding agents working in this repository.\n\n")
	fmt.Fprintf(&b, "## Project\n\n- Language: %s\n", info.Language)
	if info.BuildCommand != "" {
		fmt.Fprintf(&b, "- Build: `%s`\n", info.BuildCommand)
	}
	if info.TestCommand != "" {
		fmt.Fprintf(&b, "- Test: `%s`\n", info.TestCommand)
	}
	if info.LintCommand != "" {
		fmt.Fprintf(&b, "- Lint: `%s`\n", info.LintCommand)
	}
	b.WriteString("\n## Conventions\n\n- Run the tests before calling work done.\n")
	return b.String()
}

// InitAgents writes AGENTS.md in dir from AgentsTemplate; it never
// overwrites one.
func InitAgents(dir string) (string, error) {
	path := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Lstat(path); err == nil {
		return "", existsError{"AGENTS.md", path}
	}
	info, err := DetectProject(dir)
	if err != nil {
		return "", fmt.Errorf("project detection failed: %w", err)
	}
	if err := writeNew(path, AgentsTemplate(info)); err != nil {
		if os.IsExist(err) {
			return "", existsError{"AGENTS.md", path}
		}
		return "", fmt.Errorf("failed to write AGENTS.md: %w", err)
	}
	return path, nil
}

// writeNew creates path with content and fails if anything (a file or a
// symlink) is already there, so an init never overwrites the user's file.
// A write that fails removes the file it created, so a later init is not
// refused by a half-written one.
func writeNew(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = writeContent(f, content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// writeContent is writeNew's write (a test seam).
var writeContent = (*os.File).WriteString

// existsError is an init refusing to overwrite a file; it matches
// fs.ErrExist.
type existsError struct{ name, path string }

func (e existsError) Error() string        { return e.name + " already exists at " + e.path }
func (e existsError) Is(target error) bool { return target == fs.ErrExist }
