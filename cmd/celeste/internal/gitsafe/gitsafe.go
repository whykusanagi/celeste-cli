// Package gitsafe runs celeste's own git commands without the programs a
// repository's configuration can name. The model's sandboxed commands can
// write a repository's .git (a linked worktree shares its config and hooks
// with the repository), and celeste runs git outside the sandbox: its
// git_status, git_log and git_diff tools, the lane merge, status line and
// context snapshot. So that git runs no core.fsmonitor command and no hook,
// and a merge no configured merge driver.
package gitsafe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Args returns git's argument list for args with core.fsmonitor off and
// hooks pointed at the null device, where none can be found.
func Args(args ...string) []string {
	return append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}, args...)
}

// Command is git with Args(args...), run in dir.
func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", Args(args...)...)
	cmd.Dir = dir
	return cmd
}

// textMerge is git's own three-way text merge, as a merge driver command.
const textMerge = "git merge-file -L ours -L base -L theirs --marker-size=%L %A %O %B"

// MergeOptions returns the -c options that, given to a git merge in dir
// before the subcommand, replace every merge driver the configuration
// defines (merge.<name>.driver) with git's text merge, so the merge runs
// no program the configuration names. nil when there are none.
func MergeOptions(ctx context.Context, dir string) ([]string, error) {
	out, err := Command(ctx, dir, "config", "-z", "--name-only", "--get-regexp", `^merge\..+\.driver$`).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil // none set
		}
		return nil, fmt.Errorf("reading merge drivers: %w", err)
	}
	var opts []string
	for _, name := range strings.Split(string(out), "\x00") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, "=\n") {
			return nil, fmt.Errorf("merge driver %q cannot be overridden", name)
		}
		opts = append(opts, "-c", name+"="+textMerge)
	}
	return opts, nil
}
