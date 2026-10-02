package builtin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/proctree"
)

// shellWaitDelay bounds how long a finished or killed shell's output pipes
// may stay open (a grandchild holding them), ruling 2.
const shellWaitDelay = 2 * time.Second

// defaultShellTimeout applies when ShellOptions.Timeout is not set: bash's
// own default.
const defaultShellTimeout = 20 * time.Second

// ShellOptions is one model-chosen shell command for RunShell.
type ShellOptions struct {
	Dir     string
	Command string
	Stdin   []byte        // nil: no stdin
	Timeout time.Duration // <= 0: defaultShellTimeout
}

// ShellResult is what RunShell observed.
type ShellResult struct {
	Output    string // combined stdout+stderr, capped at maxCommandOutput
	Truncated bool
	ExitCode  int // -1 when the command was blocked, did not start or was killed
	TimedOut  bool
	Blocked   string // the denylist's reason; nothing ran
	Err       error  // start or wait error other than a non-zero exit or the timeout
}

// RunShell runs one model-chosen shell command (ruling 1): the denylist
// first, then sh -c in its own process group, killed whole on timeout. A
// background process still holding the output pipes after the shell exits
// gets shellWaitDelay before the pipes are closed; it is not killed then
// (only a timeout kills the group), and Err says what happened.
func RunShell(ctx context.Context, o ShellOptions) ShellResult {
	if reason := checkDangerousCommand(o.Command); reason != "" {
		return ShellResult{Blocked: reason, ExitCode: -1}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", o.Command)
	cmd.Dir = o.Dir
	if o.Stdin != nil {
		cmd.Stdin = bytes.NewReader(o.Stdin)
	}
	// One writer value for both streams: os/exec then shares a single pipe
	// and copy goroutine, so cappedBuffer needs no lock.
	out := &cappedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	proctree.Prepare(cmd)
	cmd.WaitDelay = shellWaitDelay
	err := cmd.Run()

	res := ShellResult{
		Output:    out.String(),
		Truncated: out.truncated,
		ExitCode:  -1,
		TimedOut:  errors.Is(cctx.Err(), context.DeadlineExceeded),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil, res.TimedOut, errors.As(err, &exitErr):
	case errors.Is(err, exec.ErrWaitDelay):
		res.Err = fmt.Errorf("the shell exited but a background process kept its output open, so later output was dropped (redirect it: cmd > log 2>&1 &): %w", err)
	default:
		res.Err = err
	}
	return res
}

// cappedBuffer keeps the first maxCommandOutput bytes and drops the rest.
type cappedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxCommandOutput - c.buf.Len(); room < len(p) {
		c.truncated = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string { return c.buf.String() }
