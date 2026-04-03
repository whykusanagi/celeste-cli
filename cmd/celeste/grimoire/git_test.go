package grimoire

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test User")

	// Create a file and commit so we have history.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644))
	run("git", "add", "hello.txt")
	run("git", "commit", "-m", "initial commit")
	return dir
}

func TestCaptureGitSnapshot_InRepo(t *testing.T) {
	dir := initTestRepo(t)
	snap := CaptureGitSnapshot(dir)
	require.NotNil(t, snap)

	// Branch should be master or main depending on git config default.
	assert.NotEmpty(t, snap.Branch)
	assert.Equal(t, "Test User", snap.UserName)
	assert.Contains(t, snap.RecentCommits, "initial commit")
}

func TestCaptureGitSnapshot_NotARepo(t *testing.T) {
	dir := t.TempDir()
	snap := CaptureGitSnapshot(dir)
	assert.Nil(t, snap)
}

func TestCaptureGitSnapshot_StatusTruncation(t *testing.T) {
	dir := initTestRepo(t)

	// Create many untracked files to produce a large status output.
	for i := 0; i < 500; i++ {
		name := filepath.Join(dir, strings.Repeat("x", 20)+string(rune('A'+i%26))+".txt")
		_ = os.WriteFile(name, []byte("data"), 0644)
	}

	snap := CaptureGitSnapshot(dir)
	require.NotNil(t, snap)

	// Status should not exceed maxStatusBytes + the truncation message.
	if len(snap.Status) > maxStatusBytes+50 {
		t.Errorf("status too large: %d bytes", len(snap.Status))
	}
}

func TestFormatForPrompt_Nil(t *testing.T) {
	var s *GitSnapshot
	assert.Equal(t, "", s.FormatForPrompt())
}

func TestFormatForPrompt_Full(t *testing.T) {
	s := &GitSnapshot{
		Branch:        "feature/test",
		MainBranch:    "main",
		Status:        "M file.go",
		RecentCommits: "abc1234 some commit",
		UserName:      "Alice",
	}
	out := s.FormatForPrompt()
	assert.Contains(t, out, "feature/test")
	assert.Contains(t, out, "Main branch: main")
	assert.Contains(t, out, "User: Alice")
	assert.Contains(t, out, "M file.go")
	assert.Contains(t, out, "abc1234 some commit")
	assert.Contains(t, out, "snapshot at conversation start and will not update")
}
