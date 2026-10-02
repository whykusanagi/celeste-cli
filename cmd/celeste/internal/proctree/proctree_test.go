//go:build unix

package proctree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The Windows tree kill (taskkill /T) is not tested here: a cmd /c start /b
// grandchild's lifetime is not observable reliably on CI runners.

func TestPrepareKillsTheWholeGroupOnCancel(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $! > pid; wait")
	cmd.Dir = dir
	Prepare(cmd)
	_ = cmd.Run()

	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("grandchild %d still exists (kill 0: %v)", pid, err)
	}
}

func TestKillBeforeStartIsANoop(t *testing.T) {
	if err := Kill(exec.Command("true")); err != nil {
		t.Fatal(err)
	}
}
