package grimoire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectProject_Go(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.26\n"), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "go", info.Language)
	assert.Equal(t, "example.com/foo", info.ModulePath)
	assert.Contains(t, info.TestCommand, "go test")
}

func TestDetectProject_Node(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "my-app", "scripts": {"test": "jest"}}`), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "javascript", info.Language)
	assert.Equal(t, "my-app", info.ModulePath)
}

func TestDetectProject_TypeScript(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "ts-app"}`), 0644)
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "typescript", info.Language)
}

func TestDetectProject_Python(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.poetry]\nname = \"myapp\"\n"), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "python", info.Language)
}

func TestDetectProject_PythonRequirements(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "python", info.Language)
}

func TestDetectProject_Rust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"myapp\"\n"), 0644)

	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "rust", info.Language)
}

func TestDetectProject_Unknown(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "unknown", info.Language)
}

func TestGenerateTemplate_Go(t *testing.T) {
	info := &ProjectInfo{
		Language:    "go",
		ModulePath:  "github.com/whykusanagi/celeste-cli",
		TestCommand: "go test ./... -count=1",
		LintCommand: "golangci-lint run ./...",
	}

	content := GenerateTemplate(info, t.TempDir())
	assert.Contains(t, content, "## Bindings")
	assert.Contains(t, content, "go project")
	assert.Contains(t, content, "github.com/whykusanagi/celeste-cli")
	assert.Contains(t, content, "## Rituals")
	assert.Contains(t, content, "## Wards")
}

func TestGenerateTemplate_Unknown(t *testing.T) {
	info := &ProjectInfo{
		Language: "unknown",
	}
	content := GenerateTemplate(info, t.TempDir())
	assert.Contains(t, content, "## Bindings")
	assert.Contains(t, content, "unknown project")
}

func TestInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.26\n"), 0644)

	path, err := Init(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".grimoire"), path)

	// Verify file was created
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "go project")
}

func TestInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".grimoire"), []byte("existing"), 0644)

	_, err := Init(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// 2.0 W4 (ruling 5): Init writes .grimoire only; .gitignore is the user's.
func TestInitLeavesGitignoreAlone(t *testing.T) {
	dir := t.TempDir()
	mk(t, filepath.Join(dir, ".gitignore"), "bin/\n")
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != "bin/\n" {
		t.Fatalf(".gitignore changed: %q", b)
	}
}

// Ruling 6: a project grimoire or a context file between the git root and
// the workspace is project context; the global grimoire and anything above
// the git root are not.
func TestHasProjectContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mk(t, filepath.Join(home, ".celeste", "grimoire.md"), "# global\n")
	outer := t.TempDir()
	mk(t, filepath.Join(outer, ".grimoire"), "# above the root\n")
	ws := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(ws, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if HasProjectContext(ws) {
		t.Fatal("only the global grimoire and one above the git root: no project context")
	}
	mk(t, filepath.Join(ws, "CLAUDE.md"), "x")
	if !HasProjectContext(ws) {
		t.Fatal("a CLAUDE.md is project context")
	}
	ws2 := filepath.Join(outer, "repo2")
	if err := os.MkdirAll(filepath.Join(ws2, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(ws2, ".celeste", "grimoire", "a.md"), "# frag\n")
	if !HasProjectContext(ws2) {
		t.Fatal("a .celeste/grimoire fragment is project context")
	}
}

func TestInitAgentsNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	mk(t, filepath.Join(dir, "go.mod"), "module x\n")
	path, err := InitAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "go test") || !strings.Contains(string(b), "go build") {
		t.Fatalf("AGENTS.md for a Go module should name its build and test commands:\n%s", b)
	}
	if _, err := InitAgents(dir); err == nil {
		t.Fatal("a second /init agents must not overwrite AGENTS.md")
	}
}
