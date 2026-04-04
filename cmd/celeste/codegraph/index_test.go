package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexer_BuildIndex(t *testing.T) {
	// Create a mini Go project
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() {
	serve()
}

func serve() {}
`)
	writeFile(t, dir, "handler.go", `package main

type Handler struct{}

func (h *Handler) Handle() {}
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	err = idx.Build()
	require.NoError(t, err)

	stats, err := idx.Stats()
	require.NoError(t, err)
	assert.Greater(t, stats.TotalSymbols, 0, "should have indexed symbols")
	assert.Greater(t, stats.TotalFiles, 0, "should have indexed files")
}

func TestIndexer_IncrementalUpdate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() {}
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	// First build
	err = idx.Build()
	require.NoError(t, err)

	stats1, _ := idx.Stats()

	// Add a new file
	writeFile(t, dir, "helper.go", `package main

func helper() string { return "help" }
`)

	// Incremental update
	err = idx.Update()
	require.NoError(t, err)

	stats2, _ := idx.Stats()
	assert.Greater(t, stats2.TotalSymbols, stats1.TotalSymbols)
}

func TestIndexer_SemanticSearch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "auth.go", `package main

// validateSession checks if the auth token is valid.
func validateSession(token string) bool { return token != "" }

// createUser registers a new user account.
func createUser(name string) {}
`)
	writeFile(t, dir, "render.go", `package main

// renderHTML generates HTML output from a template.
func renderHTML(tmpl string) string { return tmpl }
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	err = idx.Build()
	require.NoError(t, err)

	// Search for auth-related code
	results, err := idx.SemanticSearch("authentication session token", 10)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestIndexer_ProjectSummary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() {}
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	_ = idx.Build()

	summary := idx.ProjectSummary()
	assert.Contains(t, summary, "testproject")
}

func TestIndexer_SkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() {}
`)
	// Create files inside vendor dirs that should be skipped
	writeFile(t, dir, "node_modules/pkg/index.js", `function hello() {}`)
	writeFile(t, dir, "venv/lib/site.py", `def site(): pass`)
	writeFile(t, dir, "__pycache__/mod.py", `cached = True`)
	writeFile(t, dir, ".git/config", `[core]`)

	dbPath := filepath.Join(dir, "test-codegraph.db")

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	err = idx.Build()
	require.NoError(t, err)

	stats, err := idx.Stats()
	require.NoError(t, err)

	// Should only index main.go, not files in vendor dirs
	assert.Equal(t, 1, stats.TotalFiles, "should only index main.go, not vendor dir files")
}

func TestIndexer_RespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module testproject\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() {}
`)
	writeFile(t, dir, "generated.go", `package main

func generated() {}
`)
	writeFile(t, dir, ".gitignore", "generated.go\n")

	dbPath := filepath.Join(dir, "test-codegraph.db")

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	err = idx.Build()
	require.NoError(t, err)

	stats, err := idx.Stats()
	require.NoError(t, err)

	// Should only index main.go, not generated.go
	assert.Equal(t, 1, stats.TotalFiles, "should skip files matched by .gitignore")
}

func TestDefaultIndexPath(t *testing.T) {
	path := DefaultIndexPath("/some/project/root")
	assert.Contains(t, path, ".celeste")
	assert.Contains(t, path, "projects")
	assert.Contains(t, path, "codegraph.db")
	// Should not contain the project directory itself
	assert.NotContains(t, path, "/some/project/root")
	// Same input should give same output
	path2 := DefaultIndexPath("/some/project/root")
	assert.Equal(t, path, path2)
	// Different input should give different output
	path3 := DefaultIndexPath("/other/project")
	assert.NotEqual(t, path, path3)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}
