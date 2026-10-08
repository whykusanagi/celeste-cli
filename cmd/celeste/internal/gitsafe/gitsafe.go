// Package gitsafe runs celeste's own git commands without the programs a
// repository's configuration can name. The model's sandboxed commands can
// write a repository's .git (a linked worktree shares its config and hooks
// with the repository), or make a repository of their own in the
// workspace, and celeste runs git outside the sandbox: its git_status,
// git_log and git_diff tools, the lane merge, status line and context
// snapshot. So that git runs no core.fsmonitor command, no hook, no filter
// driver and no signature check, a merge no configured merge driver, and
// it never follows a commondir or .git pointer the sandbox could not keep
// read-only to a git dir whose config a command planted.
package gitsafe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
)

// Args returns git's argument list for args with core.fsmonitor off, hooks
// pointed at the null device, where none can be found, no signature check
// (gpg.program), no bare repository found by discovery, and submodules
// neither recursed into nor looked inside: a child git in a submodule
// reads that submodule's own config. (A .gitmodules ignore setting wins
// over diff.ignoreSubmodules, so status and diff also take
// --ignore-submodules=dirty.) Prepare adds the rest.
func Args(args ...string) []string {
	return append([]string{
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "log.showSignature=false",
		"-c", "safe.bareRepository=explicit",
		"-c", "submodule.recurse=false",
		"-c", "diff.ignoreSubmodules=dirty",
		"-c", "diff.submodule=short",
		"-c", "status.submoduleSummary=false",
	}, args...)
}

// Env returns the environment for git in dir: this process's, with
// GIT_DIR, GIT_COMMON_DIR and GIT_WORK_TREE naming the repository dir is
// in as sandbox.FindRepo verifies it (a linked worktree's .git file and
// admin dir must point back; a commondir in a plain .git is ignored), so
// git never takes its config from a git dir a planted commondir or .git
// names. Unchanged when the environment names a git dir already, and
// outside a repository: git then finds none itself either. A .git FindRepo
// refuses (a symlink, or a gitdir pointer that is not a linked worktree's
// or a submodule's) gets GIT_DIR set to the null device, so git does not
// rediscover it and follow the pointer to a repository the sandbox does
// not protect: it reports no repository instead (Aikido review of #422).
func Env(dir string) []string {
	env := os.Environ()
	if os.Getenv("GIT_DIR") != "" {
		return env
	}
	r, found := sandbox.FindRepo(dir)
	if !found {
		return env
	}
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_COMMON_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			out = append(out, kv)
		}
	}
	if r.GitDir == "" {
		return append(out, "GIT_DIR="+os.DevNull)
	}
	return append(out, "GIT_DIR="+r.GitDir, "GIT_COMMON_DIR="+r.CommonDir, "GIT_WORK_TREE="+r.WorkTree)
}

// refused is the error for git in dir when FindRepo refuses its .git.
func refused(dir string) error {
	if os.Getenv("GIT_DIR") != "" {
		return nil
	}
	if r, found := sandbox.FindRepo(dir); found && r.GitDir == "" {
		return fmt.Errorf("%s is not a git repository celeste runs git in: %s", r.WorkTree, r.Refusal)
	}
	return nil
}

// Prepare returns git's argument list and environment for args run in dir:
// Args, plus -c options that turn off every filter driver the repository's
// own configuration defines (its clean, smudge and process commands
// emptied, required off; git then filters nothing), and Env. Drivers from
// your global and system config, which no sandboxed command can write,
// stay (git-lfs's, typically).
func Prepare(ctx context.Context, dir string, args ...string) (argv, env []string, err error) {
	if err := refused(dir); err != nil {
		return nil, nil, err
	}
	env = Env(dir)
	keys, err := configKeys(ctx, dir, env, `^filter\..+\.(clean|smudge|process|required)$`)
	if err != nil {
		return nil, nil, err
	}
	var opts []string
	seen := map[string]bool{}
	for _, k := range keys {
		if trustedScope[k.scope] {
			continue
		}
		driver := k.name[:strings.LastIndexByte(k.name, '.')]
		if seen[driver] {
			continue
		}
		seen[driver] = true
		opts = append(opts, "-c", driver+".clean=", "-c", driver+".smudge=", "-c", driver+".process=", "-c", driver+".required=false")
	}
	return Args(append(opts, args...)...), env, nil
}

// trustedScope are the config scopes no sandboxed command can write: your
// global and system config, and -c options (celeste's own, or your
// environment's).
var trustedScope = map[string]bool{"global": true, "system": true, "command": true}

// Command is git with Prepare(ctx, dir, args...), run in dir.
func Command(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	argv, env, err := Prepare(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir, cmd.Env = dir, env
	return cmd, nil
}

// textMerge is git's own three-way text merge, as a merge driver command.
const textMerge = "git merge-file -L ours -L base -L theirs --marker-size=%L %A %O %B"

// MergeOptions returns the -c options that, given to a git merge in dir
// before the subcommand, replace every merge driver the configuration
// defines (merge.<name>.driver) with git's text merge, so the merge runs
// no program the configuration names. nil when there are none.
func MergeOptions(ctx context.Context, dir string) ([]string, error) {
	if err := refused(dir); err != nil {
		return nil, err
	}
	keys, err := configKeys(ctx, dir, Env(dir), `^merge\..+\.driver$`)
	if err != nil {
		return nil, err
	}
	var opts []string
	for _, k := range keys {
		opts = append(opts, "-c", k.name+"="+textMerge)
	}
	return opts, nil
}

// configKey is one configuration key and the scope it was set in.
type configKey struct{ scope, name string }

// configKeys returns the configuration keys in dir matching pattern, read
// by git run with env. A key that a -c option could not name (one holding
// = or a newline) is an error.
func configKeys(ctx context.Context, dir string, env []string, pattern string) ([]configKey, error) {
	cmd := exec.CommandContext(ctx, "git", Args("config", "-z", "--show-scope", "--name-only", "--get-regexp", pattern)...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil // none set
		}
		return nil, fmt.Errorf("reading git config: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	// -z --show-scope: scope NUL name NUL, for each key.
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("reading git config: unexpected output %q", out)
	}
	var keys []configKey
	for i := 0; i < len(fields); i += 2 {
		name := fields[i+1]
		if strings.ContainsAny(name, "=\n") || !strings.Contains(name, ".") {
			return nil, fmt.Errorf("git config key %q cannot be overridden", name)
		}
		keys = append(keys, configKey{fields[i], name})
	}
	return keys, nil
}
