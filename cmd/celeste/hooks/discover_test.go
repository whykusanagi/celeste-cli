package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHome points HOME and USERPROFILE at one temp dir (hermetic, all OSes).
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func skipSymlinksOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs Developer Mode or admin rights on Windows runners")
	}
}

func skipSpecialFilesOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("creating FIFOs is Unix-specific")
	}
}

const oneHookJSON = `{"hooks":[{"event":"PreToolUse","command":"check"}]}`
const grimoireWithHooks = "# Project\n\n## Hooks\n\n### PreToolUse\n- bash: echo pre\n"

func TestDiscoverClassifiesSources(t *testing.T) {
	home := testHome(t)
	writeFile(t, filepath.Join(home, ".celeste", "hooks.json"), oneHookJSON)
	writeFile(t, filepath.Join(home, ".celeste", "grimoire.md"), grimoireWithHooks)
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, ".grimoire"), grimoireWithHooks)
	ws := filepath.Join(parent, "sub")
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), oneHookJSON)

	srcs, _, err := Discover(ws, home)
	require.NoError(t, err)
	var kinds []SourceKind
	for _, s := range srcs {
		kinds = append(kinds, s.Kind)
		assert.Len(t, s.Hash, 64)
	}
	require.Equal(t, []SourceKind{KindGlobal, KindGlobalGrimoire, KindRepo, KindRepoGrimoire}, kinds)
	assert.True(t, srcs[0].Global())
	assert.True(t, srcs[1].Global())
	assert.Empty(t, srcs[0].Root, "global hooks run in the workspace")
	assert.False(t, srcs[2].Global())
	assert.Equal(t, filepath.Join(ws, ".celeste", "hooks.json"), srcs[2].Path, "trust key is the found path")
	assert.Equal(t, ws, srcs[2].Root)
	assert.False(t, srcs[3].Global(), "an ancestor grimoire is repo-local and needs approval")
	assert.Equal(t, filepath.Join(parent, ".grimoire"), srcs[3].Path)
	assert.Equal(t, parent, srcs[3].Root)
	assert.Equal(t, ProtocolV1, srcs[3].Hooks[0].Protocol)
}

func TestDiscoverGrimoireFragmentRoot(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "grimoire", "hooks.md"), grimoireWithHooks)
	srcs, _, err := Discover(ws, home)
	require.NoError(t, err)
	require.Len(t, srcs, 1)
	assert.Equal(t, ws, srcs[0].Root)
}

func TestDiscoverRequiresAbsoluteHome(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), oneHookJSON)

	for _, home := range []string{"", "."} {
		t.Run("Discover home "+home, func(t *testing.T) {
			_, _, err := Discover(ws, home)
			assert.Error(t, err)
		})
		t.Run("SourcesAt home "+home, func(t *testing.T) {
			_, _, err := SourcesAt(ws, home)
			assert.Error(t, err)
		})
	}
}

func TestDiscoverSkipsBadFileWithWarning(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), `{"hooks":[{"event":"PreToolUse","comand":"x"}]}`)
	srcs, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, srcs)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "hooks.json")
}

func TestDiscoverCapsFileSize(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), `{"hooks":[]}`+strings.Repeat(" ", 2<<20))
	srcs, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, srcs)
	assert.Contains(t, strings.Join(warnings, "\n"), "1 MiB")
}

func TestDiscoverRefusesSymlinkedRepoFile(t *testing.T) {
	skipSymlinksOnWindows(t)
	home := testHome(t)
	elsewhere := filepath.Join(t.TempDir(), "hooks.json")
	writeFile(t, elsewhere, oneHookJSON)
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(ws, ".celeste", "hooks.json")))
	srcs, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, srcs)
	assert.Contains(t, strings.Join(warnings, "\n"), "symlink")
}

func TestDiscoverRefusesSymlinkedRepoDirectory(t *testing.T) {
	skipSymlinksOnWindows(t)
	home := testHome(t)
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "hooks.json"), oneHookJSON)
	ws := t.TempDir()
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(ws, ".celeste")))
	srcs, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, srcs)
	assert.Contains(t, strings.Join(warnings, "\n"), "symlink")
}

func TestRefuseSymlinkedRepoComponentsRequiresPlainDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".celeste"), "not a directory")

	err := refuseSymlinkedRepoComponents(filepath.Join(root, ".celeste", "hooks.json"), root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlinked repo hook file")
}

func TestDiscoverAllowsSymlinkedGlobalHookFile(t *testing.T) {
	skipSymlinksOnWindows(t)
	home := testHome(t)
	target := filepath.Join(t.TempDir(), "hooks.json")
	writeFile(t, target, oneHookJSON)
	global := filepath.Join(home, ".celeste", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(global), 0o755))
	require.NoError(t, os.Symlink(target, global))
	ws := t.TempDir()

	srcs, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, srcs, 1)
	assert.Equal(t, KindGlobal, srcs[0].Kind)
	require.Len(t, srcs[0].Hooks, 1)
	assert.Equal(t, "check", srcs[0].Hooks[0].Command)
}

func TestDiscoverRefusesSymlinkedGlobalHookFileToNonRegularTarget(t *testing.T) {
	skipSpecialFilesOnWindows(t)
	skipSymlinksOnWindows(t)
	home := testHome(t)
	target := filepath.Join(t.TempDir(), "hooks.fifo")
	require.NoError(t, exec.Command("mkfifo", target).Run())
	global := filepath.Join(home, ".celeste", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(global), 0o755))
	require.NoError(t, os.Symlink(target, global))
	ws := t.TempDir()

	type result struct {
		srcs     []Source
		warnings []string
		err      error
	}
	done := make(chan result, 1)
	go func() {
		srcs, warnings, err := Discover(ws, home)
		done <- result{srcs: srcs, warnings: warnings, err: err}
	}()

	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.Empty(t, got.srcs)
		assert.Contains(t, strings.Join(got.warnings, "\n"), "not a regular file")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Discover blocked opening a non-regular global hook file")
	}
}

func TestDiscoverRefusesNonRegularRepoFile(t *testing.T) {
	skipSpecialFilesOnWindows(t)
	home := testHome(t)
	ws := t.TempDir()
	p := filepath.Join(ws, ".celeste", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, exec.Command("mkfifo", p).Run())

	type result struct {
		srcs     []Source
		warnings []string
		err      error
	}
	done := make(chan result, 1)
	go func() {
		srcs, warnings, err := Discover(ws, home)
		done <- result{srcs: srcs, warnings: warnings, err: err}
	}()

	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.Empty(t, got.srcs)
		assert.Contains(t, strings.Join(got.warnings, "\n"), "not a regular file")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Discover blocked opening a non-regular repo hook file")
	}
}

func TestDiscoverHomeWorkspaceCountsGlobalOnce(t *testing.T) {
	home := testHome(t)
	writeFile(t, filepath.Join(home, ".celeste", "hooks.json"), oneHookJSON)
	srcs, _, err := Discover(home, home)
	require.NoError(t, err)
	require.Len(t, srcs, 1)
	assert.Equal(t, KindGlobal, srcs[0].Kind)
}

func TestDiscoverWarnsOnV1(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".grimoire"), grimoireWithHooks)
	_, warnings, err := Discover(ws, home)
	require.NoError(t, err)
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, "protocol v1")
	assert.Contains(t, joined, "MIGRATING-2.0.md")
}

func TestDiscoverIgnoresGrimoireWithoutHooks(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".grimoire"), "# P\n\n## Bindings\n- Go\n")
	srcs, _, err := Discover(ws, home)
	require.NoError(t, err)
	assert.Empty(t, srcs)
}

func TestSourcesAtFileAndDir(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	p := filepath.Join(ws, ".celeste", "hooks.json")
	writeFile(t, p, oneHookJSON)
	srcs, _, err := SourcesAt(p, home)
	require.NoError(t, err)
	require.Len(t, srcs, 1)
	assert.Equal(t, KindRepo, srcs[0].Kind)
	assert.Equal(t, ws, srcs[0].Root)
	srcs, _, err = SourcesAt(ws, home)
	require.NoError(t, err)
	require.Len(t, srcs, 1)
	_, _, err = SourcesAt(filepath.Join(ws, "missing.json"), home)
	assert.Error(t, err)
}

func TestSourcesAtRejectsUnknownFiles(t *testing.T) {
	home := testHome(t)
	ws := t.TempDir()
	for _, name := range []string{"notes.md", filepath.Join("random", "hooks.json"), "hooks.json"} {
		p := filepath.Join(ws, name)
		writeFile(t, p, oneHookJSON)
		_, _, err := SourcesAt(p, home)
		assert.Error(t, err, name)
	}
}
