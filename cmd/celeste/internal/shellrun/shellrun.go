// Package shellrun is the one runner for every sh -c command celeste starts:
// the bash tool (through builtin.RunShell, which checks the denylist
// first), user-authored custom tools and the agent's --verify-cmd; Args
// runs a program directly (the agent's artifact git calls). Each run
// gets its own process group, killed whole on timeout, a bounded wait for
// whatever still holds the output pipe, and an output cap.
package shellrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/proctree"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
)

// WaitDelay bounds how long a finished or killed shell's output pipes may
// stay open (a grandchild holding them).
const WaitDelay = 2 * time.Second

// DefaultTimeout applies when Options.Timeout is not set.
const DefaultTimeout = 20 * time.Second

// DefaultMaxOutput applies when Options.MaxOutput is not set.
const DefaultMaxOutput = 64_000

// Options is one shell command for Run.
type Options struct {
	Dir       string
	Command   string        // run with sh -c
	Args      []string      // non-empty: run Args[0] with Args[1:] directly, no shell; Command is ignored
	Stdin     []byte        // nil: no stdin
	Env       []string      // nil: this process's environment
	Timeout   time.Duration // <= 0: DefaultTimeout
	MaxOutput int           // <= 0: DefaultMaxOutput
	// Policy, when enabled and an OS sandbox is available, runs Command
	// inside it (2.0 W4). nil or disabled: plain sh -c. Ignored with Args.
	Policy *sandbox.Policy
}

// Result is what Run observed.
type Result struct {
	Output    string // combined stdout+stderr, capped at MaxOutput
	Truncated bool
	ExitCode  int // -1 when the command was blocked, did not start or was killed
	TimedOut  bool
	Blocked   string // set only by callers that check a denylist first; nothing ran
	Err       error  // start or wait error other than a non-zero exit or the timeout
	Sandbox   string // the OS sandbox the command ran in ("seatbelt", "bubblewrap"); "" for none
	Hint      string // a failed sandboxed command that looks blocked: the sandbox and the key to change
}

// Run runs Command with sh -c (or Args directly) in its own process group, killed whole on
// timeout. It applies no denylist: callers that run model-chosen commands
// check one first (builtin.RunShell). A background process still holding
// the output pipes after the shell exits gets WaitDelay, whatever the
// shell's exit status; then the group is killed, the pipe is closed, and
// Err says so. A background process that redirected its output does not
// hold the pipes and keeps running.
func Run(ctx context.Context, o Options) Result {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	limit := o.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	var kind string
	args := o.Args
	if len(args) == 0 && o.Policy != nil {
		args, kind = sandbox.Wrap(*o.Policy, o.Command)
	}
	if len(args) > 0 {
		cmd = exec.CommandContext(cctx, args[0], args[1:]...)
	} else {
		cmd = exec.CommandContext(cctx, "sh", "-c", o.Command)
	}
	cmd.Dir, cmd.Env = o.Dir, o.Env
	if o.Stdin != nil {
		cmd.Stdin = bytes.NewReader(o.Stdin)
	}
	// Run owns the output pipe rather than relying on Cmd.WaitDelay: Wait
	// reports exec.ErrWaitDelay only when the shell exited zero, so a
	// failing shell would let a background holder of the pipe escape.
	// cappedBuffer is written only by the copy goroutine below.
	out := &cappedBuffer{limit: limit}
	r, w, err := os.Pipe()
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	defer r.Close()
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = WaitDelay // bounds the stdin copy goroutine
	var watched []sandbox.PathState
	if kind != "" {
		watched = sandbox.SnapshotPaths(o.Policy.Watch)
		// Sandboxed: also out of celeste's terminal session (no /dev/tty).
		err = proctree.StartSession(cmd)
	} else {
		err = proctree.Start(cmd)
	}
	_ = w.Close()
	if err != nil {
		return Result{ExitCode: -1, Err: err, Sandbox: kind}
	}
	defer proctree.Release(cmd) // after Wait, the drain and any Kill below
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, r)
		close(done)
	}()
	err = cmd.Wait()

	heldPipe := false
	select {
	case <-done:
	case <-time.After(WaitDelay):
		// Whatever still holds the pipe is in the group; its output can no
		// longer reach anyone, so it does not outlive the call unseen.
		heldPipe = true
		_ = proctree.Kill(cmd)
		_ = r.Close()
		<-done
	}

	res := Result{
		Output:    out.String(),
		Truncated: out.truncated,
		ExitCode:  -1,
		TimedOut:  errors.Is(cctx.Err(), context.DeadlineExceeded),
		Sandbox:   kind,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if kind != "" && res.ExitCode != 0 {
		res.Hint = sandbox.Hint(kind, *o.Policy, res.Output)
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
	// Put back what the sandbox could not keep read-only (Policy.Watch).
	if rerr := sandbox.RestorePaths(watched); rerr != nil {
		res.Err = errors.Join(res.Err, rerr)
	}
	return res
}

// cappedBuffer keeps the first limit bytes and drops the rest.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room < len(p) {
		c.truncated = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string { return c.buf.String() }
