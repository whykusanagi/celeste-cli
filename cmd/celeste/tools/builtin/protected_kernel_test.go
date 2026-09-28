package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Fix round 4: the kernel-level (os.SameFile) half of hook-file protection,
// driven through the real registry in trust mode, the way the model reaches
// these tools.

func trustRegistry(t *testing.T, workspace string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	RegisterAll(reg, workspace, nil, nil, nil)
	reg.SetPermissionChecker(permissions.NewChecker(permissions.PermissionConfig{Mode: permissions.ModeTrust}))
	return reg
}

func execTool(t *testing.T, reg *tools.Registry, name string, input map[string]any) tools.ToolResult {
	t.Helper()
	res, err := reg.Execute(context.Background(), name, input)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func wantProtected(t *testing.T, res tools.ToolResult) {
	t.Helper()
	if !res.Error || !strings.Contains(res.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", res)
	}
}

func skipSymlinksOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// ~/.celeste/hooks.json -> ~/L/<rest>, ~/L -> <workspace>/newdir, and
// newdir does not exist yet: writing newdir/<rest> would create the file the
// hook loader reads.
func TestWriteFileKernelCheckDanglingDirectorySymlink(t *testing.T) {
	skipSymlinksOnWindows(t)
	for _, rest := range []string{filepath.Join("x", "hooks.json"), "hooks.json"} {
		t.Run(rest, func(t *testing.T) {
			home := setProtectedHome(t)
			workspace := t.TempDir()
			hooks := filepath.Join(home, ".celeste", "hooks.json")
			mustSymlink(t, filepath.Join(home, "L", rest), hooks)
			mustSymlink(t, filepath.Join(workspace, "newdir"), filepath.Join(home, "L"))

			reg := trustRegistry(t, workspace)
			wantProtected(t, execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("newdir", rest),
				"content": `{"planted":true}`,
			}))

			if _, err := os.Stat(hooks); !os.IsNotExist(err) {
				t.Fatalf("hooks.json resolves after refused write (err %v)", err)
			}
			if _, err := os.Lstat(filepath.Join(workspace, "newdir")); !os.IsNotExist(err) {
				t.Fatalf("refused write left newdir behind (err %v)", err)
			}

			// splice_file dest creates a file too (no mkdir): pre-create
			// the directories and check it is caught after writing.
			if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, "newdir", rest)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "src.txt"), []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			wantProtected(t, execTool(t, reg, "splice_file", map[string]any{
				"op":         "copy",
				"source":     "src.txt",
				"dest":       filepath.Join("newdir", rest),
				"start_line": 1,
				"end_line":   1,
			}))
			if _, err := os.Stat(hooks); !os.IsNotExist(err) {
				t.Fatalf("hooks.json resolves after refused splice (err %v)", err)
			}
		})
	}
}

// hooks.json -> "S/../hooks-real.json" with S a symlinked directory: the
// kernel resolves ".." against S's target, filepath.Clean does not.
func TestWriteFileKernelCheckDotDotAfterSymlinkedDir(t *testing.T) {
	skipSymlinksOnWindows(t)
	home := setProtectedHome(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	celeste := filepath.Join(home, ".celeste")
	mustSymlink(t, filepath.Join(workspace, "sub", "deeper"), filepath.Join(celeste, "S"))
	// Link text kept verbatim: filepath.Join would Clean "S/.." away.
	mustSymlink(t, "S"+string(filepath.Separator)+".."+string(filepath.Separator)+"hooks-real.json", filepath.Join(celeste, "hooks.json"))

	reg := trustRegistry(t, workspace)
	wantProtected(t, execTool(t, reg, "write_file", map[string]any{
		"path":    filepath.Join("sub", "hooks-real.json"),
		"content": `{"planted":true}`,
	}))
	if _, err := os.Stat(filepath.Join(celeste, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json resolves after refused write (err %v)", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "sub", "hooks-real.json")); !os.IsNotExist(err) {
		t.Fatalf("refused write left a stray file (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "sub", "deeper")); err != nil {
		t.Fatalf("pre-existing directory removed: %v", err)
	}
}

// An existing protected file reached through an alias no path comparison
// can see (a hard link) is refused before anything is written.
func TestMutatingToolsKernelCheckExistingAlias(t *testing.T) {
	home := setProtectedHome(t)
	workspace := t.TempDir()
	hooks := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "{\"hooks\":{}}\n"
	if err := os.WriteFile(hooks, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "alias.json")
	if err := os.Link(hooks, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := trustRegistry(t, workspace)
	calls := []struct {
		name  string
		input map[string]any
	}{
		{"write_file", map[string]any{"path": "alias.json", "content": "planted"}},
		{"write_file", map[string]any{"path": "alias.json", "content": "planted", "append": true}},
		{"patch_file", map[string]any{"path": "alias.json", "old_string": "{}", "new_string": "planted"}},
		{"splice_file", map[string]any{"op": "copy", "source": "src.txt", "dest": "alias.json", "start_line": 1, "end_line": 1}},
		{"splice_file", map[string]any{"op": "move", "source": "alias.json", "dest": "src.txt", "start_line": 1, "end_line": 1}},
	}
	for _, c := range calls {
		wantProtected(t, execTool(t, reg, c.name, c.input))
		got, err := os.ReadFile(hooks)
		if err != nil || string(got) != original {
			t.Fatalf("%s %v changed hooks.json: %q, %v", c.name, c.input, got, err)
		}
	}
}

func TestMutatingToolsKernelCheckAllowsUnrelatedWrites(t *testing.T) {
	home := setProtectedHome(t)
	workspace := t.TempDir()
	hooks := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := trustRegistry(t, workspace)
	for _, c := range []struct {
		name  string
		input map[string]any
	}{
		{"write_file", map[string]any{"path": "a/b/new.txt", "content": "one\n"}},
		{"write_file", map[string]any{"path": "a/b/new.txt", "content": "two\n", "append": true}},
		{"patch_file", map[string]any{"path": "a/b/new.txt", "old_string": "two", "new_string": "three"}},
		{"splice_file", map[string]any{"op": "copy", "source": "a/b/new.txt", "dest": "a/copy.txt", "start_line": 1, "end_line": 1}},
		{"write_file", map[string]any{"path": ".celeste/hooks.json", "content": "{}"}}, // repo hooks
	} {
		if res := execTool(t, reg, c.name, c.input); res.Error {
			t.Fatalf("%s %v: %+v, want success", c.name, c.input, res)
		}
	}
	got, err := os.ReadFile(filepath.Join(workspace, "a", "b", "new.txt"))
	if err != nil || string(got) != "one\nthree\n" {
		t.Fatalf("new.txt = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "a", "copy.txt")); err != nil {
		t.Fatalf("splice dest missing: %v", err)
	}
}

// Fix round 5: ~/L -> <workspace>/d/sub (sub missing) and hooks.json ->
// ~/L/../evil.json. Planting d/evil.json is harmless while hooks.json
// dangles; creating sub (by writing d/sub/.keep) would make hooks.json
// resolve to it, so that second write is refused and undone.
func TestWriteFileRefusesDirectoryCreationThatResolvesHookFile(t *testing.T) {
	skipSymlinksOnWindows(t)
	for _, spelling := range []string{"raw", "resolved"} {
		t.Run(spelling, func(t *testing.T) {
			home := setProtectedHome(t)
			workspace := t.TempDir()
			if spelling == "resolved" {
				// macOS: /var/... vs /private/var/...
				real, err := filepath.EvalSymlinks(workspace)
				if err != nil {
					t.Fatal(err)
				}
				workspace = real
			}
			if err := os.MkdirAll(filepath.Join(workspace, "d"), 0o755); err != nil {
				t.Fatal(err)
			}
			hooks := filepath.Join(home, ".celeste", "hooks.json")
			mustSymlink(t, filepath.Join(workspace, "d", "sub"), filepath.Join(home, "L"))
			// Link text kept verbatim: filepath.Join would Clean "L/.." away.
			mustSymlink(t, filepath.Join(home, "L")+string(filepath.Separator)+".."+string(filepath.Separator)+"evil.json", hooks)

			reg := trustRegistry(t, workspace)
			if res := execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("d", "evil.json"),
				"content": `{"planted":true}`,
			}); res.Error {
				t.Fatalf("call 1 = %+v, want success (hooks.json still dangles)", res)
			}
			wantProtected(t, execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("d", "sub", ".keep"),
				"content": "keep\n",
			}))
			if _, err := os.Lstat(filepath.Join(workspace, "d", "sub")); !os.IsNotExist(err) {
				t.Fatalf("refused write left sub behind (err %v)", err)
			}
			if _, err := os.Stat(hooks); err == nil {
				t.Fatal("hooks.json resolves after refused write")
			}

			// A nested write creating directories unrelated to any
			// protected name still succeeds.
			if res := execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("other", "deep", "x.txt"),
				"content": "ok\n",
			}); res.Error {
				t.Fatalf("unrelated nested write = %+v, want success", res)
			}
			if _, err := os.Stat(filepath.Join(workspace, "other", "deep", "x.txt")); err != nil {
				t.Fatalf("unrelated nested write missing: %v", err)
			}
		})
	}
}

// Fix round 6: a write that fails after MkdirAll (here the final name is
// too long, so Lstat reports it missing, MkdirAll succeeds and the write
// itself fails) must still undo the directories it created. Otherwise the
// round-5 setup plants a hook file through that error path.
func TestWriteFileErrorAfterMkdirAllRemovesCreatedDirs(t *testing.T) {
	skipSymlinksOnWindows(t)
	longName := strings.Repeat("a", 300)
	for _, appendMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("append=%v", appendMode), func(t *testing.T) {
			home := setProtectedHome(t)
			workspace := t.TempDir()
			if err := os.MkdirAll(filepath.Join(workspace, "d"), 0o755); err != nil {
				t.Fatal(err)
			}
			hooks := filepath.Join(home, ".celeste", "hooks.json")
			mustSymlink(t, filepath.Join(workspace, "d", "sub"), filepath.Join(home, "L"))
			mustSymlink(t, filepath.Join(home, "L")+string(filepath.Separator)+".."+string(filepath.Separator)+"evil.json", hooks)

			reg := trustRegistry(t, workspace)
			if res := execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("d", "evil.json"),
				"content": "PLANT",
			}); res.Error {
				t.Fatalf("call 1 = %+v, want success (hooks.json still dangles)", res)
			}
			res := execTool(t, reg, "write_file", map[string]any{
				"path":    filepath.Join("d", "sub", longName),
				"content": "x",
				"append":  appendMode,
			})
			if !res.Error {
				t.Fatalf("call 2 = %+v, want an error", res)
			}
			if _, err := os.Lstat(filepath.Join(workspace, "d", "sub")); !os.IsNotExist(err) {
				t.Fatalf("failed write left sub behind (err %v)", err)
			}
			if data, err := os.ReadFile(hooks); err == nil {
				t.Fatalf("hooks.json resolves after failed write: %q", data)
			}
		})
	}
}

// An ordinary write that fails after MkdirAll, with no symlinks involved,
// leaves none of the directories it created behind.
func TestWriteFileErrorAfterMkdirAllLeavesNoDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("name-length limits differ on Windows")
	}
	setProtectedHome(t)
	workspace := t.TempDir()
	reg := trustRegistry(t, workspace)
	for _, appendMode := range []bool{false, true} {
		res := execTool(t, reg, "write_file", map[string]any{
			"path":    filepath.Join("n1", "n2", strings.Repeat("b", 300)),
			"content": "x",
			"append":  appendMode,
		})
		if !res.Error {
			t.Fatalf("append=%v: write = %+v, want an error", appendMode, res)
		}
		if _, err := os.Lstat(filepath.Join(workspace, "n1")); !os.IsNotExist(err) {
			t.Fatalf("append=%v: failed write left n1 behind (err %v)", appendMode, err)
		}
	}
}
