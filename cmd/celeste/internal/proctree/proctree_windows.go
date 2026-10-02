//go:build windows

// Package proctree runs a command as the root of its own process tree and
// kills the whole tree: hooks and the shell tools share it, so a timeout
// takes down whatever the command started as well.
package proctree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Prepare makes cancelling cmd's context kill its whole process tree. It
// leaves SysProcAttr alone (callers may set CmdLine). Call it before Start.
func Prepare(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return Kill(cmd) }
}

// Kill ends cmd's process tree with taskkill /T /F, then the root itself.
// A command that never started, or has already exited, is not an error.
//
// There is no Job Object: if the root has already exited, Windows may no
// longer expose every grandchild relationship to taskkill /T, so this is
// best-effort for already-detached descendants.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	taskkill := "taskkill"
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		taskkill = filepath.Join(systemRoot, "System32", "taskkill.exe")
	}
	_ = exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
