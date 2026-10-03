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

	"golang.org/x/sys/unix"
)

func TestStartKillsTheWholeGroupOnCancel(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $! > pid; wait")
	cmd.Dir = dir
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

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

// Review Minor 4: StartSession leaves the caller's session, so the command
// has no controlling terminal to type into (TIOCSTI), and is still killed
// as a whole group.
func TestStartSessionLeavesTheSessionAndKillsTheGroup(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $! > pid; wait")
	cmd.Dir = dir
	if err := StartSession(cmd); err != nil {
		t.Fatal(err)
	}
	sid, err := unix.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != cmd.Process.Pid {
		t.Fatalf("session = %d, want the command's own (%d)", sid, cmd.Process.Pid)
	}
	var pid int
	for deadline := time.Now().Add(2 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(dir, "pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if pid == 0 {
		t.Fatal("no pid recorded")
	}
	cancel()
	_ = cmd.Wait()
	if !gone(pid, 2*time.Second) {
		t.Fatalf("grandchild %d survived the cancel", pid)
	}
}
