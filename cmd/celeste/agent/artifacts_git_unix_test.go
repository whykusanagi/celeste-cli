//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/gittest"
)

func TestCaptureGitWorkspaceArtifactsReadsStatusAndDiff(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "f.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, diff := captureGitWorkspaceArtifacts(dir, 10*time.Second)
	if !strings.Contains(status, "f.txt") || !strings.Contains(diff, "+b") {
		t.Fatalf("status = %q, diff = %q", status, diff)
	}
	if s, d := captureGitWorkspaceArtifacts(t.TempDir(), 10*time.Second); s != "" || d != "" {
		t.Fatalf("outside a repo: %q %q", s, d)
	}
}

// git runs directly (no sh -c) with a bounded wait on its output pipe: a
// child of git holding stdout does not keep the artifact bundle waiting.
func TestCaptureGitWorkspaceArtifactsDoesNotWaitOnAPipeHolder(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\ncase \"$1\" in\nrev-parse) echo true ;;\nstatus) sleep 15 & echo ' M f.txt' ;;\ndiff) echo '+b' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	status, diff := captureGitWorkspaceArtifacts(t.TempDir(), 30*time.Second)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("took %s waiting on a pipe holder", took)
	}
	if !strings.Contains(status, "M f.txt") || !strings.Contains(diff, "+b") {
		t.Fatalf("status = %q, diff = %q", status, diff)
	}
}

// A failing git whose child still holds stdout is bounded too, and the
// child is killed with the group (review of cleanup-5c).
func TestCaptureGitWorkspaceArtifactsKillsAPipeHolderAfterAFailingGit(t *testing.T) {
	bin := t.TempDir()
	pidfile := filepath.Join(t.TempDir(), "pid")
	fake := "#!/bin/sh\ncase \"$1\" in\nrev-parse) echo true ;;\nstatus) sleep 15 & echo $! > '" + pidfile + "'; exit 1 ;;\ndiff) echo '+b' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	captureGitWorkspaceArtifacts(t.TempDir(), 30*time.Second)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("took %s waiting on a pipe holder", took)
	}
	b, _ := os.ReadFile(pidfile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("no pid recorded: %q", b)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("the child holding git's pipe survived a failing git")
	}
}

// A large diff reaches the bundle whole: runGit's cap is far above the
// runner's 64 KB default (review of cleanup-5c).
func TestCaptureGitWorkspaceArtifactsKeepsALargeDiffWhole(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	var before, after strings.Builder
	for i := 0; i < 10000; i++ {
		before.WriteString("line " + strconv.Itoa(i) + "\n")
		after.WriteString("line " + strconv.Itoa(i) + " changed\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(before.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "f.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(after.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diff := captureGitWorkspaceArtifacts(dir, 30*time.Second)
	if len(diff) <= 64_000 {
		t.Fatalf("diff cut to %d bytes", len(diff))
	}
	if !strings.HasSuffix(diff, "+line 9999 changed\n") {
		t.Fatalf("diff does not end with the last hunk line: %q", diff[len(diff)-80:])
	}
}

// A diff over the cap says so, so a cut patch is never taken for a whole one.
func TestCaptureGitWorkspaceArtifactsMarksATruncatedDiff(t *testing.T) {
	old := gitMaxOutput
	gitMaxOutput = 1000
	t.Cleanup(func() { gitMaxOutput = old })
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("a\n", 2000)), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "f.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("b\n", 2000)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diff := captureGitWorkspaceArtifacts(dir, 30*time.Second)
	if !strings.HasSuffix(diff, "\n# celeste: output truncated at 1000 bytes\n") {
		t.Fatalf("no truncation trailer: %q", diff[max(0, len(diff)-120):])
	}
}
