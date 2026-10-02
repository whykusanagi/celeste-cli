//go:build unix

// Package proctree runs a command as the root of its own process tree and
// kills the whole tree: hooks and the shell tools share it, so a timeout
// takes down whatever the command started as well.
package proctree

import (
	"errors"
	"os/exec"
	"syscall"
)

// Start starts cmd in a new process group and makes cancelling its context
// kill the whole group. Use it instead of cmd.Start; it keeps any other
// SysProcAttr fields already set.
// cmd must come from exec.CommandContext.
func Start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return Kill(cmd) }
	return cmd.Start()
}

// Kill sends SIGKILL to cmd's process group. A command that never started,
// or whose group is already gone, is not an error.
//
// Called after Wait, the group leader has been reaped; the kernel cannot
// recycle a pgid while any member of the group still exists, so the only
// residual risk is an unrelated new group leader that reused this exact
// pid after the group emptied.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// Release lets go of what Start kept for Kill. A process group needs
// nothing, so on unix it does nothing; call it anyway, after Wait and any
// Kill, for Windows.
func Release(cmd *exec.Cmd) {}
