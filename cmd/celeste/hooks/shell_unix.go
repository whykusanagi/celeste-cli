//go:build unix

package hooks

import (
	"context"
	"os/exec"
	"syscall"
)

// envValueCap is the largest CELESTE_* value passed: Linux rejects a single
// environment string over 128 KiB (MAX_ARG_STRLEN), less headroom for the name.
const envValueCap = 120 << 10

// shellCommand runs a v2 hook with sh -c in its own process group, so a
// timeout kills whatever the hook started as well.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd
}

// v1Command runs a converted grimoire hook the way 1.x did: sh -c.
func v1Command(ctx context.Context, command string) *exec.Cmd { return shellCommand(ctx, command) }
