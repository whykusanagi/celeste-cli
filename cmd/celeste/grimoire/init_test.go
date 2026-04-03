package grimoire

import (
	"os"
	"path/filepath"
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

	content := GenerateTemplate(info)
	assert.Contains(t, content, "## Bindings")
	assert.Contains(t, content, "Go")
	assert.Contains(t, content, "github.com/whykusanagi/celeste-cli")
	assert.Contains(t, content, "## Rituals")
	assert.Contains(t, content, "## Wards")
}

func TestGenerateTemplate_Unknown(t *testing.T) {
	info := &ProjectInfo{
		Language: "unknown",
	}
	content := GenerateTemplate(info)
	assert.Contains(t, content, "## Bindings")
	assert.Contains(t, content, "Describe your project")
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
	assert.Contains(t, string(data), "Go project")
}

func TestInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".grimoire"), []byte("existing"), 0644)

	_, err := Init(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
