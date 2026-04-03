package grimoire

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveIncludes_BasicFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ARCH.md"), []byte("# Architecture\nThis is the arch doc."), 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./ARCH.md"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	assert.Contains(t, g.Incantations[0].Content, "Architecture")
	assert.Empty(t, g.Incantations[0].Error)
}

func TestResolveIncludes_MissingFile(t *testing.T) {
	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./nonexistent.md"},
		},
	}

	err := ResolveIncludes(g, t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, g.Incantations[0].Error)
}

func TestResolveIncludes_CycleDetection(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("@./b.md\ncontent a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("@./a.md\ncontent b"), 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./a.md"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	// Should resolve a.md but detect cycle when b.md tries to include a.md again
	assert.Contains(t, g.Incantations[0].Content, "content a")
}

func TestResolveIncludes_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	// Create chain: d0.md -> d1.md -> d2.md -> d3.md -> d4.md
	for i := 0; i < 5; i++ {
		content := ""
		if i < 4 {
			content = fmt.Sprintf("@./d%d.md\n", i+1)
		}
		content += fmt.Sprintf("depth %d", i)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("d%d.md", i)), []byte(content), 0644)
	}

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./d0.md"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	// Should contain d0 content
	assert.Contains(t, g.Incantations[0].Content, "depth 0")
}

func TestResolveIncludes_SizeCap(t *testing.T) {
	dir := t.TempDir()
	// Create a file larger than 25KB
	bigContent := strings.Repeat("x", 30*1024)
	os.WriteFile(filepath.Join(dir, "huge.md"), []byte(bigContent), 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./huge.md"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	// Should have error about size limit
	assert.NotEmpty(t, g.Incantations[0].Error)
	assert.Contains(t, g.Incantations[0].Error, "limit")
}

func TestResolveIncludes_BinaryRejected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "binary.dat"), []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}, 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./binary.dat"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	assert.NotEmpty(t, g.Incantations[0].Error)
	assert.Contains(t, g.Incantations[0].Error, "binary")
}

func TestResolveIncludes_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("content A"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("content B"), 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./a.md"},
			{Path: "@./b.md"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	assert.Contains(t, g.Incantations[0].Content, "content A")
	assert.Contains(t, g.Incantations[1].Content, "content B")
}

func TestResolveIncludes_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "image.png"), []byte("fake png"), 0644)

	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./image.png"},
		},
	}

	err := ResolveIncludes(g, dir)
	require.NoError(t, err)
	assert.NotEmpty(t, g.Incantations[0].Error)
	assert.Contains(t, g.Incantations[0].Error, "unsupported")
}
