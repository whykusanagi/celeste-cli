package grimoire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		c := &langCandidate{language: lang, testCmd: "npm test", buildCmd: "npm run build", hasManifest: true}
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

// GenerateTemplate creates a starter .grimoire file from project info.
func GenerateTemplate(info *ProjectInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Grimoire: %s project\n\n", info.Language))

	// Bindings
	sb.WriteString("## Bindings\n")
	switch info.Language {
	case "go":
		sb.WriteString("- This is a Go project\n")
		if info.ModulePath != "" {
			sb.WriteString(fmt.Sprintf("- Module path: %s\n", info.ModulePath))
		}
		sb.WriteString("- Use standard library conventions\n")
	case "javascript", "typescript":
		lang := "JavaScript"
		if info.Language == "typescript" {
			lang = "TypeScript"
		}
		sb.WriteString(fmt.Sprintf("- This is a %s project\n", lang))
		if info.ModulePath != "" {
			sb.WriteString(fmt.Sprintf("- Package name: %s\n", info.ModulePath))
		}
	case "python":
		sb.WriteString("- This is a Python project\n")
	case "rust":
		sb.WriteString("- This is a Rust project\n")
	default:
		sb.WriteString("- Describe your project here\n")
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

// Init creates a .grimoire file in the given directory.
// Returns the path to the created file, or error if one already exists.
func Init(dir string) (string, error) {
	grimPath := filepath.Join(dir, ".grimoire")

	// Check if .grimoire already exists
	if _, err := os.Stat(grimPath); err == nil {
		return "", fmt.Errorf(".grimoire already exists at %s", grimPath)
	}

	info, err := DetectProject(dir)
	if err != nil {
		return "", fmt.Errorf("project detection failed: %w", err)
	}

	content := GenerateTemplate(info)
	if err := os.WriteFile(grimPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write .grimoire: %w", err)
	}

	// Append .celeste/ to .gitignore if not already there
	gitignorePath := filepath.Join(dir, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		if !strings.Contains(string(data), ".celeste/") {
			f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				_, _ = f.WriteString("\n# Celeste CLI local data\n.celeste/\n")
				f.Close()
			}
		}
	}

	return grimPath, nil
}
