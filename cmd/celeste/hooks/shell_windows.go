//go:build windows

package hooks

import (
	"context"
	"os"
	"os/exec"
	"syscall"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/proctree"
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
	proctree.Prepare(cmd)
	return cmd
}

// v1Command runs a converted grimoire hook the way 1.x did: sh -c. It needs a
// POSIX sh on PATH (Git for Windows ships one).
func v1Command(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	proctree.Prepare(cmd)
	return cmd
}
