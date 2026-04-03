package grimoire

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover_SingleFile(t *testing.T) {
	dir := t.TempDir()
	grimPath := filepath.Join(dir, ".grimoire")
	err := os.WriteFile(grimPath, []byte("## Bindings\n- test project\n"), 0644)
	require.NoError(t, err)

	paths, err := Discover(dir)
	require.NoError(t, err)

	// Find the project-level source (filter out any global)
	var projectSources []GrimoireSource
	for _, p := range paths {
		if p.Priority >= PriorityProject {
			projectSources = append(projectSources, p)
		}
	}
	assert.Len(t, projectSources, 1)
	assert.Equal(t, grimPath, projectSources[0].Path)
}

func TestDiscover_LocalOverridesProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".grimoire"), []byte("## Bindings\n- base\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".grimoire.local"), []byte("## Bindings\n- local override\n"), 0644)

	paths, err := Discover(dir)
	require.NoError(t, err)

	// Find project-level sources
	var projectSources []GrimoireSource
	for _, p := range paths {
		if p.Priority >= PriorityProject {
			projectSources = append(projectSources, p)
		}
	}
	assert.True(t, len(projectSources) >= 2)
	// .grimoire.local should have higher priority than .grimoire
	var projectPrio, localPrio int
	for _, s := range projectSources {
		if filepath.Base(s.Path) == ".grimoire" {
			projectPrio = s.Priority
		}
		if filepath.Base(s.Path) == ".grimoire.local" {
			localPrio = s.Priority
		}
	}
	assert.Greater(t, localPrio, projectPrio)
}

func TestDiscover_FragmentDirectory(t *testing.T) {
	dir := t.TempDir()
	fragDir := filepath.Join(dir, ".celeste", "grimoire")
	os.MkdirAll(fragDir, 0755)
	os.WriteFile(filepath.Join(fragDir, "go.md"), []byte("## Bindings\n- Go conventions\n"), 0644)
	os.WriteFile(filepath.Join(fragDir, "testing.md"), []byte("## Rituals\n- Always test\n"), 0644)

	paths, err := Discover(dir)
	require.NoError(t, err)

	var fragSources []GrimoireSource
	for _, p := range paths {
		if p.Priority >= PriorityFragment {
			fragSources = append(fragSources, p)
		}
	}
	assert.True(t, len(fragSources) >= 2)
}

func TestDiscover_NoGrimoire(t *testing.T) {
	dir := t.TempDir()
	paths, err := Discover(dir)
	require.NoError(t, err)

	// Filter out global (may or may not exist on test machine)
	var nonGlobal []GrimoireSource
	for _, p := range paths {
		if p.Priority > PriorityGlobal {
			nonGlobal = append(nonGlobal, p)
		}
	}
	assert.Empty(t, nonGlobal)
}

func TestDiscover_ParentDirectory(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "subdir")
	os.MkdirAll(child, 0755)
	os.WriteFile(filepath.Join(parent, ".grimoire"), []byte("## Bindings\n- parent project\n"), 0644)

	paths, err := Discover(child)
	require.NoError(t, err)

	var found bool
	for _, p := range paths {
		if p.Path == filepath.Join(parent, ".grimoire") {
			found = true
			break
		}
	}
	assert.True(t, found, "should discover .grimoire in parent directory")
}
