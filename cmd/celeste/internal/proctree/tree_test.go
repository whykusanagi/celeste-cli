package proctree

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the process tree: PROCTREE_HELPER=parent starts
// a grandchild (PROCTREE_HELPER=sleep) that inherits its stdout, writes the
// grandchild's pid to PROCTREE_PIDFILE, and then sleeps (or, with orphan,
// exits at once and leaves the grandchild running).
func TestMain(m *testing.M) {
	switch os.Getenv("PROCTREE_HELPER") {
	case "parent", "orphan":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "PROCTREE_HELPER=sleep")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("PROCTREE_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(4)
		}
		if os.Getenv("PROCTREE_HELPER") == "parent" {
			time.Sleep(20 * time.Second)
		}
		os.Exit(0)
	case "sleep":
		// Bounded, so an escaped helper can't outlive the test run by much.
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperTree(t *testing.T, ctx context.Context, mode string) (*exec.Cmd, string) {
	t.Helper()
	pidfile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), "PROCTREE_HELPER="+mode, "PROCTREE_PIDFILE="+pidfile)
	return cmd, pidfile
}

func readPid(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second) // a cold Windows runner starts binaries slowly
	for {
		b, err := os.ReadFile(pidfile)
		if err == nil && len(b) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper never wrote its grandchild's pid: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Cancelling kills the grandchild too, so a Wait that reads the output pipe
// the grandchild inherited returns at once, with no WaitDelay to cut it
// short (hooks on Windows: cmd.exe and the program it runs).
func TestStartCancelKillsTheTreeAndWaitReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, pidfile := helperTree(t, ctx, "parent")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, pidfile)
	start := time.Now()
	cancel()
	_ = cmd.Wait()
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Wait took %s after cancel; the grandchild kept the output open", d)
	}
	if !gone(pid, 5*time.Second) {
		t.Errorf("grandchild %d still running after cancel", pid)
	}
}

// Kill after Wait still ends what the command left running: a hook's
// background child once the hook's shell has exited.
func TestKillAfterWaitEndsWhatTheCommandLeftRunning(t *testing.T) {
	cmd, pidfile := helperTree(t, context.Background(), "orphan")
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, pidfile)
	if err := Kill(cmd); err != nil {
		t.Fatal(err)
	}
	if !gone(pid, 5*time.Second) {
		t.Errorf("grandchild %d still running after Kill", pid)
	}
}

func TestKillBeforeStartIsANoop(t *testing.T) {
	if err := Kill(exec.Command(os.Args[0])); err != nil {
		t.Fatal(err)
	}
}

// Release is idempotent, and a Kill after it is a no-op rather than an
// error (on Windows the job handle is closed by then).
func TestReleaseIsIdempotentAndKillAfterItIsANoop(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^$")
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	Release(cmd)
	Release(cmd)
	if err := Kill(cmd); err != nil {
		t.Fatal(err)
	}
	Release(exec.Command(os.Args[0])) // never started
}
