package gitsafe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
)

func TestMain(m *testing.M) {
	gittest.Isolate()
	os.Exit(m.Run())
}

func TestArgsTurnOffRepositoryPrograms(t *testing.T) {
	got := Args("status", "--short")
	for _, want := range []string{"core.fsmonitor=false", "core.hooksPath=" + os.DevNull, "log.showSignature=false", "safe.bareRepository=explicit", "submodule.recurse=false"} {
		if i := slices.Index(got, want); i < 1 || got[i-1] != "-c" {
			t.Errorf("Args = %v, lacks -c %s", got, want)
		}
	}
	if !slices.Equal(got[len(got)-2:], []string{"status", "--short"}) {
		t.Fatalf("Args = %v: the subcommand is not last", got)
	}
}

func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Review Important 1: celeste's git names the git dir it verified, so a
// commondir planted in a plain .git (git takes its config from the dir it
// names) is never followed.
func TestEnvNamesTheVerifiedGitDir(t *testing.T) {
	repo := sandbox.Resolve(t.TempDir())
	gittest.Run(t, repo, "init", "-q")
	gitDir := filepath.Join(repo, ".git")
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../.fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "a")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	env := Env(sub)
	for key, want := range map[string]string{"GIT_DIR": gitDir, "GIT_COMMON_DIR": gitDir, "GIT_WORK_TREE": repo} {
		if got, _ := envValue(env, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := envValue(Env(t.TempDir()), "GIT_DIR"); ok {
		t.Error("GIT_DIR set outside a repository")
	}
}

// The probe: a planted commondir pointing at a git dir whose config
// defines a filter, and a filter defined in the repository's own config
// (a repository a sandboxed command made itself), run nothing through
// celeste's git. A filter from your global config still runs.
func TestCommandRunsNoPlantedFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the filter is a sh command")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := sandbox.Resolve(t.TempDir())
	marker := filepath.Join(t.TempDir(), "ran")
	filter := "touch '" + marker + "'; cat"
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, repo, "init", "-q")
	write(filepath.Join(repo, "f"), "a\n")
	gittest.Run(t, repo, "add", "f")
	gittest.Run(t, repo, "commit", "-q", "-m", "a")
	write(filepath.Join(repo, ".gitattributes"), "f filter=x\n")
	write(filepath.Join(repo, "f"), "a\nb\n")
	run := func(args ...string) {
		t.Helper()
		cmd, err := Command(context.Background(), repo, args...)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	ran := func() bool {
		_, err := os.Stat(marker)
		_ = os.Remove(marker)
		return err == nil
	}

	// A planted commondir: a copy of the git dir with the filter in its
	// config. Plain git follows it (the reviewer's probe).
	fake := filepath.Join(repo, ".fake")
	if out, err := exec.Command("cp", "-R", filepath.Join(repo, ".git"), fake).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	gittest.Run(t, fake, "config", "--file", filepath.Join(fake, "config"), "filter.x.clean", filter)
	write(filepath.Join(repo, ".git", "commondir"), "../.fake\n")
	gittest.Run(t, repo, "diff", "--stat")
	if !ran() {
		t.Skip("this git does not follow the planted commondir")
	}
	run("diff", "--stat")
	run("status", "--short")
	if ran() {
		t.Fatal("celeste's git followed the planted commondir and ran its filter")
	}
	if err := os.Remove(filepath.Join(repo, ".git", "commondir")); err != nil {
		t.Fatal(err)
	}

	// The repository's own config.
	gittest.Run(t, repo, "config", "filter.x.clean", filter)
	gittest.Run(t, repo, "config", "filter.x.required", "true")
	run("diff", "--stat")
	run("add", "f")
	if ran() {
		t.Fatal("celeste's git ran the repository's filter")
	}
	gittest.Run(t, repo, "config", "--unset", "filter.x.clean")
	gittest.Run(t, repo, "config", "--unset", "filter.x.required")

	// Your global config is yours: its filter stays.
	global := filepath.Join(t.TempDir(), "gitconfig")
	write(global, "[filter \"x\"]\n\tclean = "+filter+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	write(filepath.Join(repo, "f"), "a\nb\nc\n")
	run("diff", "--stat")
	if !ran() {
		t.Fatal("the global config's filter did not run")
	}
}
