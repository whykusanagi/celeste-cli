//go:build unix

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The verify command's grandchild holding the output pipe no longer
// outlives the timeout: the whole process group is killed and the call
// returns instead of waiting for the grandchild (a go test child, say).
func TestVerifyCommandGrandchildDoesNotOutliveTheTimeout(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	check := executeVerificationCommand(context.Background(), dir, "sleep 15 & echo $! > pid; wait", 500*time.Millisecond)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("the verify command took %s past a 500ms timeout", took)
	}
	if !check.TimedOut || check.Passed {
		t.Fatalf("check = %+v", check)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "pid"))
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
		t.Fatal("the verify command's grandchild outlived the timeout")
	}
}

func TestVerifyCommandReportsExitAndCapsOutput(t *testing.T) {
	dir := t.TempDir()
	check := executeVerificationCommand(context.Background(), dir, "echo ok", 5*time.Second)
	if !check.Passed || check.ExitCode != 0 || check.Output != "ok\n" {
		t.Fatalf("check = %+v", check)
	}
	check = executeVerificationCommand(context.Background(), dir, "head -c 50000 /dev/zero | tr '\\0' x; exit 2", 5*time.Second)
	if check.Passed || check.ExitCode != 2 || len(check.Output) != maxCommandOutput {
		t.Fatalf("passed=%v exit=%d len=%d", check.Passed, check.ExitCode, len(check.Output))
	}
}
