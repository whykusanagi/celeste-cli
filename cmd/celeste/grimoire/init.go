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

	// Check for Go project
	goModPath := filepath.Join(dir, "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		info.Language = "go"
		info.TestCommand = "go test ./... -count=1"
		info.LintCommand = "golangci-lint run ./..."
		info.BuildCommand = "go build ./..."
		// Parse module path from go.mod
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				info.ModulePath = strings.TrimSpace(strings.TrimPrefix(line, "module"))
				break
			}
		}
		return info, nil
	}

	// Check for Node.js project
	pkgPath := filepath.Join(dir, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		info.Language = "javascript"
		info.TestCommand = "npm test"
		info.BuildCommand = "npm run build"

		// Check for TypeScript
		tsConfigPath := filepath.Join(dir, "tsconfig.json")
		if _, err := os.Stat(tsConfigPath); err == nil {
			info.Language = "typescript"
		}

		// Parse package.json for name and scripts
		var pkg map[string]interface{}
		if err := json.Unmarshal(data, &pkg); err == nil {
			if name, ok := pkg["name"].(string); ok {
				info.ModulePath = name
			}
			if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
				if test, ok := scripts["test"].(string); ok {
					info.TestCommand = test
				}
				if lint, ok := scripts["lint"].(string); ok {
					info.LintCommand = lint
				}
			}
		}
		return info, nil
	}

	// Check for Python project
	pyProjectPath := filepath.Join(dir, "pyproject.toml")
	requirementsPath := filepath.Join(dir, "requirements.txt")
	if _, err := os.Stat(pyProjectPath); err == nil {
		info.Language = "python"
		info.TestCommand = "pytest"
		info.LintCommand = "ruff check ."
		return info, nil
	}
	if _, err := os.Stat(requirementsPath); err == nil {
		info.Language = "python"
		info.TestCommand = "pytest"
		info.LintCommand = "ruff check ."
		return info, nil
	}

	// Check for Rust project
	cargoPath := filepath.Join(dir, "Cargo.toml")
	if _, err := os.Stat(cargoPath); err == nil {
		info.Language = "rust"
		info.TestCommand = "cargo test"
		info.LintCommand = "cargo clippy"
		info.BuildCommand = "cargo build"
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
		sb.WriteString(fmt.Sprintf("- This is a Go project\n"))
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

	return grimPath, nil
}
