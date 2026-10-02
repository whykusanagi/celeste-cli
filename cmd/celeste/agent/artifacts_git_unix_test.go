//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"strings"
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
