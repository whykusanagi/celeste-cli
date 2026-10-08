package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeRepo makes a repository's common dir at repo/.git (HEAD, objects)
// and returns it.
func fakeRepo(t *testing.T, repo string) string {
	t.Helper()
	common := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(common, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(common, "HEAD"), "ref: refs/heads/main\n")
	return common
}

// fakeLane makes a linked worktree at lane of the repository whose common
// dir is common, as git worktree add does: the lane's .git file names the
// admin dir common/worktrees/<name>, whose gitdir file points back and
// whose commondir is "../..". It returns the admin dir.
func fakeLane(t *testing.T, common, lane, name string) string {
	t.Helper()
	admin := filepath.Join(common, "worktrees", name)
	writeFile(t, filepath.Join(lane, ".git"), "gitdir: "+admin+"\n")
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(lane, ".git")+"\n")
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/"+name+"\n")
	return admin
}

// A linked worktree's git dir and the repository's common dir are outside
// the worktree: git add and git commit write there.
func TestGitDirsOfALinkedWorktree(t *testing.T) {
	repo := Resolve(t.TempDir())
	common := fakeRepo(t, repo)
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	gitdir := fakeLane(t, common, lane, "fire")

	got := GitDirs(filepath.Join(lane, "sub")) // a subdirectory finds it too
	if !slices.Equal(got, []string{common, gitdir}) {
		t.Fatalf("GitDirs = %v", got)
	}
}

func TestGitDirsOfAPlainRepoAndNoRepo(t *testing.T) {
	repo := Resolve(t.TempDir())
	common := fakeRepo(t, repo)
	if got := GitDirs(filepath.Join(repo, "a", "b")); !slices.Equal(got, []string{common}) {
		t.Fatalf("GitDirs(subdir) = %v", got)
	}
	// A submodule: a relative gitdir: line resolves against the .git file's
	// directory, and the module's core.worktree points back.
	sub := filepath.Join(repo, "mod")
	module := filepath.Join(common, "modules", "mod")
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/mod\n")
	writeFile(t, filepath.Join(module, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(module, "config"), "[core]\n\tbare = false\n\tworktree = ../../../mod\n")
	if err := os.MkdirAll(filepath.Join(module, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GitDirs(sub); !slices.Equal(got, []string{module}) {
		t.Fatalf("GitDirs(submodule) = %v", got)
	}
	if got := GitDirs(t.TempDir()); len(got) != 0 {
		// t.TempDir is under the system temp dir, never inside a repository.
		t.Fatalf("GitDirs(no repo) = %v", got)
	}
}

// Aikido 806869303: a .git the workspace's own content controls names
// nothing outside it. A symlinked .git, a gitdir: line naming a directory
// that is not a linked worktree's or a submodule's git dir pointing back
// here, and a commondir that is not the admin dir's repository are all
// refused.
func TestGitDirsRefusesGitMetadataThatDoesNotPointBack(t *testing.T) {
	outside := Resolve(t.TempDir()) // stands in for / or the home directory
	if err := os.MkdirAll(filepath.Join(outside, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "HEAD"), "ref: refs/heads/main\n")

	t.Run("symlinked .git", func(t *testing.T) {
		ws := Resolve(t.TempDir())
		if err := os.Symlink(outside, filepath.Join(ws, ".git")); err != nil {
			t.Skip("no symlinks here:", err)
		}
		if got := GitDirs(ws); len(got) != 0 {
			t.Fatalf("GitDirs = %v, want none", got)
		}
	})
	t.Run("gitdir names an arbitrary directory", func(t *testing.T) {
		for _, target := range []string{string(filepath.Separator), outside} {
			ws := Resolve(t.TempDir())
			writeFile(t, filepath.Join(ws, ".git"), "gitdir: "+target+"\n")
			if got := GitDirs(ws); len(got) != 0 {
				t.Fatalf("gitdir: %s: GitDirs = %v, want none", target, got)
			}
		}
	})
	t.Run("admin dir that points elsewhere", func(t *testing.T) {
		repo := Resolve(t.TempDir())
		common := fakeRepo(t, repo)
		real := filepath.Join(repo, "real")
		admin := fakeLane(t, common, real, "x")
		ws := Resolve(t.TempDir())
		writeFile(t, filepath.Join(ws, ".git"), "gitdir: "+admin+"\n")
		if got := GitDirs(ws); len(got) != 0 {
			t.Fatalf("GitDirs = %v, want none: the admin dir belongs to another worktree", got)
		}
	})
	t.Run("forged commondir", func(t *testing.T) {
		repo := Resolve(t.TempDir())
		common := fakeRepo(t, repo)
		lane := filepath.Join(repo, "lane")
		admin := fakeLane(t, common, lane, "lane")
		writeFile(t, filepath.Join(admin, "commondir"), outside+"\n")
		if got := GitDirs(lane); len(got) != 0 {
			t.Fatalf("GitDirs = %v, want none", got)
		}
	})
	t.Run("commondir in a plain .git dir", func(t *testing.T) {
		repo := Resolve(t.TempDir())
		common := fakeRepo(t, repo)
		writeFile(t, filepath.Join(common, "commondir"), outside+"\n")
		if got := GitDirs(repo); !slices.Equal(got, []string{common}) {
			t.Fatalf("GitDirs = %v, want only %s", got, common)
		}
	})
	t.Run("submodule whose worktree is elsewhere", func(t *testing.T) {
		repo := Resolve(t.TempDir())
		common := fakeRepo(t, repo)
		module := filepath.Join(common, "modules", "m")
		writeFile(t, filepath.Join(module, "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(module, "config"), "[core]\n\tworktree = ../../../other\n")
		ws := filepath.Join(repo, "m")
		writeFile(t, filepath.Join(ws, ".git"), "gitdir: ../.git/modules/m\n")
		if got := GitDirs(ws); len(got) != 0 {
			t.Fatalf("GitDirs = %v, want none", got)
		}
	})
}

// Review Important 1: git commit in a linked worktree works under the
// sandbox when the git dirs are writable.
func TestGitCommitInALinkedWorktreeUnderTheSandbox(t *testing.T) {
	if _, ok := Available(); !ok {
		t.Skip("no OS sandbox here")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	home := Resolve(t.TempDir())
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo := Resolve(t.TempDir())
	run(repo, "init", "-q", "-b", "main")
	run(repo, "commit", "-q", "--allow-empty", "-m", "root")
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	run(repo, "worktree", "add", "-q", "-b", "fire", lane)

	// The lane and its git dirs only: every test directory is under the temp
	// dir, which DefaultWritable allows, so it would hide a missing git dir.
	p := Policy{Enabled: true, Workspace: lane, Network: true,
		Writable: Normalize(append([]string{lane}, GitDirs(lane)...))}
	argv, _ := Wrap(p, "echo hi > f.txt && git add f.txt && git commit -q -m lane")
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = lane, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandboxed git commit: %v\n%s", err, out)
	}
	cmd = exec.Command(gitBin, "log", "--format=%s", "-1", "fire")
	cmd.Dir, cmd.Env = repo, env
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "lane" {
		t.Fatalf("the lane's commit is missing: %q %v", out, err)
	}
}

// Aikido 806869318: each git dir's config and hooks stay read-only inside
// the sandbox, the workspace's own .git included, so a sandboxed command
// cannot plant a program that git outside the sandbox later runs. Commits
// still work.
func TestGitConfigAndHooksAreReadOnlyUnderTheSandbox(t *testing.T) {
	ws := Resolve(t.TempDir())
	gitDir := filepath.Join(ws, ".git")
	for _, d := range []string{filepath.Join(gitDir, "hooks"), filepath.Join(gitDir, "objects")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(gitDir, "config"), "[core]\n")
	ro := GitProtected([]string{gitDir})
	for _, want := range []string{filepath.Join(gitDir, "config"), filepath.Join(gitDir, "hooks")} {
		if !slices.Contains(ro, want) {
			t.Fatalf("GitProtected = %v, lacks %s", ro, want)
		}
	}
	p := Policy{Enabled: true, Workspace: ws, Writable: []string{ws, gitDir}, ReadOnly: ro}

	prof := Profile(p)
	allow := strings.Index(prof, "(allow file-write*")
	deny := strings.LastIndex(prof, "(deny file-write*")
	if allow < 0 || deny < allow || !strings.Contains(prof[deny:], sbplQuote(filepath.Join(gitDir, "config"))) ||
		!strings.Contains(prof[deny:], sbplQuote(filepath.Join(gitDir, "hooks"))) {
		t.Fatalf("the profile does not deny writes to config and hooks after the allow block:\n%s", prof)
	}

	args := BwrapArgs(p, "true")
	lastRW, firstRO := -1, -1
	for i := 0; i+2 < len(args); i++ {
		if args[i] == "--bind" {
			lastRW = i
		}
		if args[i] == "--ro-bind" && args[i+1] == filepath.Join(gitDir, "config") && firstRO < 0 {
			firstRO = i
		}
	}
	if firstRO < 0 || firstRO < lastRW || !slices.Contains(args, filepath.Join(gitDir, "hooks")) {
		t.Fatalf("bwrap does not bind config and hooks read-only after the writable binds: %v", args)
	}

	if _, ok := Available(); !ok {
		return
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return
	}
	home := Resolve(t.TempDir())
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	repo := Resolve(t.TempDir())
	cmd := exec.Command(gitBin, "init", "-q", "-b", "main", repo)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	dirs := GitDirs(repo)
	p = Policy{Enabled: true, Workspace: repo, Network: true,
		Writable: Normalize(append([]string{repo}, dirs...)), ReadOnly: GitProtected(dirs)}
	run := func(command string) error {
		argv, _ := Wrap(p, command)
		c := exec.Command(argv[0], argv[1:]...)
		c.Dir, c.Env = repo, env
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("%s: %v\n%s", command, err, out)
		}
		return err
	}
	if run("git config core.fsmonitor true") == nil {
		t.Error("a sandboxed command changed .git/config")
	}
	if run("echo '#!/bin/sh' > .git/hooks/pre-commit") == nil {
		t.Error("a sandboxed command wrote a hook")
	}
	if run("mv .git .git-moved") == nil {
		t.Error("a sandboxed command moved the git dir aside")
	}
	if err := run("echo hi > f.txt && git add f.txt && git commit -q -m sandboxed"); err != nil {
		t.Errorf("git commit under the sandbox: %v", err)
	}
}

// bubblewrap binds only over paths that exist, so the protected paths a
// git dir lacks are created first (empty), and then bound read-only;
// config itself is never created.
func TestMakeGitProtectedCreatesWhatBwrapBinds(t *testing.T) {
	gitDir := Resolve(t.TempDir())
	MakeGitProtected([]string{gitDir, filepath.Join(gitDir, "missing")})
	if info, err := os.Stat(filepath.Join(gitDir, "hooks")); err != nil || !info.IsDir() {
		t.Fatalf("hooks not created: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(gitDir, "config.worktree")); err != nil || len(b) != 0 {
		t.Fatalf("config.worktree = %q, %v; want an empty file", b, err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "config")); err == nil {
		t.Fatal("config was created")
	}
	if _, err := os.Stat(filepath.Join(gitDir, "missing")); err == nil {
		t.Fatal("a missing git dir was created")
	}
	writeFile(t, filepath.Join(gitDir, "config.worktree"), "[core]\n")
	MakeGitProtected([]string{gitDir})
	if b, _ := os.ReadFile(filepath.Join(gitDir, "config.worktree")); string(b) != "[core]\n" {
		t.Fatalf("an existing config.worktree was changed: %q", b)
	}
	args := BwrapArgs(Policy{Writable: []string{gitDir}, ReadOnly: GitProtected([]string{gitDir})}, "true")
	if !slices.Contains(args, filepath.Join(gitDir, "config.worktree")) {
		t.Fatalf("config.worktree is not bound read-only: %v", args)
	}
}

// Review Important 1: git takes its config from the dir a commondir names,
// in any git dir, and follows a .git file to its git dir, so those
// pointers stay read-only too, and are watched where bubblewrap cannot
// bind them (missing).
func TestGitPointersAreProtected(t *testing.T) {
	repo := Resolve(t.TempDir())
	common := fakeRepo(t, repo)
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	admin := fakeLane(t, common, lane, "fire")

	ro := GitProtected([]string{common})
	for _, name := range []string{"commondir", "gitdir"} {
		if !slices.Contains(ro, filepath.Join(common, name)) {
			t.Errorf("GitProtected = %v, lacks %s", ro, name)
		}
	}

	want := map[string][]string{
		repo: {filepath.Join(common, "commondir"), filepath.Join(common, "gitdir")},
		lane: {filepath.Join(common, "commondir"), filepath.Join(common, "gitdir"),
			filepath.Join(admin, "commondir"), filepath.Join(admin, "gitdir"), filepath.Join(lane, ".git")},
		// A workspace below the repository's root: a .git planted in it
		// would be found first.
		filepath.Join(repo, "sub"): {filepath.Join(common, "commondir"), filepath.Join(common, "gitdir"),
			filepath.Join(repo, "sub", ".git")},
	}
	for ws, w := range want {
		slices.Sort(w)
		if got := GitPointers(ws); !slices.Equal(got, w) {
			t.Errorf("GitPointers(%s) = %v, want %v", ws, got, w)
		}
	}
	if got := GitPointers(t.TempDir()); got != nil {
		t.Errorf("GitPointers(no repo) = %v", got)
	}

	// bubblewrap binds the ones that exist read-only after the writable
	// binds: the lane's .git file and its admin dir's commondir and gitdir.
	p := Policy{Writable: []string{lane, admin, common}, ReadOnly: Normalize(append(GitProtected([]string{admin, common}), GitPointers(lane)...))}
	args := BwrapArgs(p, "true")
	lastRW := -1
	ro2 := map[string]int{}
	for i := 0; i+2 < len(args); i++ {
		switch args[i] {
		case "--bind":
			lastRW = i
		case "--ro-bind":
			ro2[args[i+1]] = i
		}
	}
	for _, path := range []string{filepath.Join(lane, ".git"), filepath.Join(admin, "commondir"), filepath.Join(admin, "gitdir")} {
		if i, ok := ro2[path]; !ok || i < lastRW {
			t.Errorf("bwrap does not bind %s read-only after the writable binds: %v", path, args)
		}
	}
	// The profile denies them, missing or not.
	prof := Profile(p)
	deny := strings.LastIndex(prof, "(deny file-write*")
	for _, path := range []string{filepath.Join(lane, ".git"), filepath.Join(admin, "commondir"), filepath.Join(common, "commondir")} {
		if !strings.Contains(prof[deny:], sbplQuote(path)) {
			t.Errorf("the profile does not deny %s:\n%s", path, prof)
		}
	}
}

// Review Important 1, probed: under the sandbox a command can neither
// point the workspace's git dir at a planted one (.git/commondir) nor
// repoint a linked worktree's .git file or its admin dir. bubblewrap
// cannot bind a missing commondir; the runner puts it back afterwards
// (shellrun), so only seatbelt is held to refusing that write here.
func TestGitPointersAreReadOnlyUnderTheSandbox(t *testing.T) {
	kind, ok := Available()
	if !ok {
		t.Skip("no OS sandbox here")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	home := Resolve(t.TempDir())
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo := Resolve(t.TempDir())
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "root")
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	git(repo, "worktree", "add", "-q", "-b", "fire", lane)

	policy := func(ws string) Policy {
		dirs := GitDirs(ws)
		ptrs := GitPointers(ws)
		return Policy{Enabled: true, Workspace: ws, Network: true,
			Writable: Normalize(append([]string{ws}, dirs...)),
			ReadOnly: Normalize(append(GitProtected(dirs), ptrs...)), Watch: ptrs}
	}
	run := func(ws, command string) error {
		argv, _ := Wrap(policy(ws), command)
		c := exec.Command(argv[0], argv[1:]...)
		c.Dir, c.Env = ws, env
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("%s: %v\n%s", command, err, out)
		}
		return err
	}
	if kind == KindSeatbelt {
		if run(repo, "mkdir .fake && echo ../.fake > .git/commondir") == nil {
			t.Error("a sandboxed command wrote .git/commondir")
		}
		if _, err := os.Lstat(filepath.Join(repo, ".git", "commondir")); err == nil {
			t.Error(".git/commondir exists")
		}
	}
	if run(lane, "echo 'gitdir: ../../../.fake' > .git") == nil {
		t.Error("a sandboxed command rewrote the lane's .git file")
	}
	admin := filepath.Join(repo, ".git", "worktrees", "fire")
	if run(lane, "echo ../../../.fake > "+admin+"/commondir") == nil {
		t.Error("a sandboxed command rewrote the admin dir's commondir")
	}
	if run(lane, "echo hi > f.txt && git add f.txt && git commit -q -m lane") != nil {
		t.Error("git commit in the lane failed under the sandbox")
	}
}

// A refused .git says why; an accepted one has no Refusal.
func TestFindRepoSaysWhyItRefused(t *testing.T) {
	outside := Resolve(t.TempDir())
	if err := os.MkdirAll(filepath.Join(outside, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "HEAD"), "ref: refs/heads/main\n")

	repo := Resolve(t.TempDir())
	common := fakeRepo(t, repo)
	moved := Resolve(t.TempDir())
	admin := fakeLane(t, common, filepath.Join(repo, "old"), "old")
	writeFile(t, filepath.Join(moved, ".git"), "gitdir: "+admin+"\n")

	cases := map[string]struct {
		ws   string
		want string
	}{
		"gitdir names a non-worktree git dir": {func() string {
			ws := Resolve(t.TempDir())
			writeFile(t, filepath.Join(ws, ".git"), "gitdir: "+outside+"\n")
			return ws
		}(), "--separate-git-dir"},
		"gitdir names nothing": {func() string {
			ws := Resolve(t.TempDir())
			writeFile(t, filepath.Join(ws, ".git"), "nothing\n")
			return ws
		}(), "names no git dir"},
		"moved linked worktree": {moved, "git worktree repair"},
	}
	ws := Resolve(t.TempDir())
	if err := os.Symlink(outside, filepath.Join(ws, ".git")); err == nil {
		cases["symlinked .git"] = struct {
			ws   string
			want string
		}{ws, "symlink"}
	}
	for name, c := range cases {
		r, found := FindRepo(c.ws)
		if !found || r.GitDir != "" || !strings.Contains(r.Refusal, c.want) {
			t.Errorf("%s: FindRepo = %+v, %v; want refused with %q", name, r, found, c.want)
		}
	}
	if r, _ := FindRepo(repo); r.GitDir == "" || r.Refusal != "" {
		t.Errorf("plain repository: FindRepo = %+v", r)
	}
}
