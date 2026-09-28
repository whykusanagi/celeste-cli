//go:build windows

package hooks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// envValueCap keeps each CELESTE_* value well below Windows' per-variable
// 32,767-character limit on Vista and later.
const envValueCap = 8 << 10

// shellCommand runs a v2 hook with cmd.exe. /s /c "…" hands the command line
// over verbatim instead of through Go's argv escaping, which cmd.exe doesn't
// understand. A timeout kills the whole tree: cmd.exe's children would
// otherwise outlive it and hold the workspace open.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmd := exec.CommandContext(ctx, comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + comspec + `" /d /s /c "` + command + `"`}
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	return cmd
}

// v1Command runs a converted grimoire hook the way 1.x did: sh -c. It needs a
// POSIX sh on PATH (Git for Windows ships one).
func v1Command(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	return cmd
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	taskkill := "taskkill"
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		taskkill = filepath.Join(systemRoot, "System32", "taskkill.exe")
	}
	// F0 has no Job Object. If the root has already exited, Windows may no
	// longer expose every grandchild relationship to taskkill /T; that accepted
	// limitation means this is best-effort for already-detached descendants.
	_ = exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
