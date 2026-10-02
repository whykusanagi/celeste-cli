package builtin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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
// gets shellWaitDelay, whatever the shell's exit status; then the group is
// killed, the pipe is closed, and Err says so. A background process that redirected its output does not
// hold the pipes and keeps running.
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
	// RunShell owns the output pipe rather than relying on Cmd.WaitDelay:
	// Wait reports exec.ErrWaitDelay only when the shell exited zero, so a
	// failing shell would let a background holder of the pipe escape.
	// cappedBuffer is written only by the copy goroutine below.
	out := &cappedBuffer{}
	r, w, err := os.Pipe()
	if err != nil {
		return ShellResult{ExitCode: -1, Err: err}
	}
	defer r.Close()
	cmd.Stdout, cmd.Stderr = w, w
	proctree.Prepare(cmd)
	cmd.WaitDelay = shellWaitDelay // bounds the stdin copy goroutine
	err = cmd.Start()
	_ = w.Close()
	if err != nil {
		return ShellResult{ExitCode: -1, Err: err}
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, r)
		close(done)
	}()
	err = cmd.Wait()

	heldPipe := false
	select {
	case <-done:
	case <-time.After(shellWaitDelay):
		// Whatever still holds the pipe is in the group; its output can no
		// longer reach anyone, so it does not outlive the call unseen.
		heldPipe = true
		_ = proctree.Kill(cmd)
		_ = r.Close()
		<-done
	}

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
	case res.TimedOut:
	case heldPipe:
		res.Err = errors.New("the shell exited but a background process kept its output open; it was stopped (redirect its output to keep it running: cmd > log 2>&1 &)")
	case err == nil, errors.As(err, &exitErr):
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
