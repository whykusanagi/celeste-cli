package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		file     string
		expected string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"server.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"style.css", "css"},
		{"page.html", "html"},
		{"config.yaml", "yaml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"unknown.xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			assert.Equal(t, tt.expected, DetectLanguage(tt.file))
		})
	}
}

func TestDetectProjectLanguage(t *testing.T) {
	dir := t.TempDir()

	// Create a go.mod file
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	assert.Equal(t, "go", DetectProjectLanguage(dir))

	// Create a package.json
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "package.json"), []byte("{}"), 0644)
	assert.Equal(t, "javascript", DetectProjectLanguage(dir2))
}

func TestIsIndexableFile(t *testing.T) {
	assert.True(t, IsIndexableFile("main.go"))
	assert.True(t, IsIndexableFile("app.py"))
	assert.True(t, IsIndexableFile("server.ts"))
	assert.False(t, IsIndexableFile("image.png"))
	assert.False(t, IsIndexableFile("data.bin"))
	assert.False(t, IsIndexableFile(".gitignore"))
}
