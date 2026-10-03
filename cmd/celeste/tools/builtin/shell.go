package builtin

import (
	"context"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellrun"
)

// ShellOptions is one model-chosen shell command for RunShell.
type ShellOptions = shellrun.Options

// ShellResult is what RunShell observed.
type ShellResult = shellrun.Result

// RunShell runs one model-chosen shell command (ruling 1): the denylist
// first, then shellrun.Run (own process group, killed whole on timeout,
// bounded pipe wait, output cap). User-authored commands (custom tools,
// --verify-cmd) are trusted input and call shellrun.Run directly.
func RunShell(ctx context.Context, o ShellOptions) ShellResult {
	if reason := checkDangerousCommand(o.Command); reason != "" {
		return ShellResult{Blocked: reason, ExitCode: -1}
	}
	if o.MaxOutput <= 0 {
		o.MaxOutput = maxCommandOutput
	}
	return shellrun.Run(ctx, o)
}
