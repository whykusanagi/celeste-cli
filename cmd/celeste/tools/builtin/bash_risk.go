package builtin

import (
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellparse"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
)

// destructiveVerbs are commands that delete, overwrite or kill on their
// own. The permission prompt rates a bash call that runs one destructive.
var destructiveVerbs = map[string]bool{
	"rm": true, "rmdir": true, "unlink": true, "shred": true, "dd": true,
	"truncate": true, "wipefs": true, "kill": true, "pkill": true, "killall": true,
}

// RiskLevel rates a bash call for the permission prompt (tools.RiskRater):
// "destructive" when the command deletes, overwrites or kills (anything the
// bash tool refuses, the destructive-bash rule fires on, or a destructive
// verb anywhere in the line, nested shells included), else "write". A call
// with no readable command is rated destructive.
func (t *BashTool) RiskLevel(input map[string]any) string {
	cmd, ok := input["command"].(string)
	if !ok || strings.TrimSpace(cmd) == "" {
		return "destructive"
	}
	if checkDangerousCommand(cmd) != "" || rules.Destructive(cmd) {
		return "destructive"
	}
	if shellparse.Walk(cmd, destructiveCommand) != shellparse.None {
		return "destructive"
	}
	return "write"
}

// destructiveSubcommands are tools whose named subcommand deletes or
// destroys: kubectl delete, docker rm, terraform destroy and the like.
var destructiveSubcommands = map[string]map[string]bool{
	"kubectl":   {"delete": true},
	"docker":    {"rm": true, "rmi": true, "prune": true, "kill": true},
	"podman":    {"rm": true, "rmi": true, "prune": true, "kill": true},
	"terraform": {"destroy": true},
	"tofu":      {"destroy": true},
}

// timeoutValueOpts are timeout's options that take the next word as value.
var timeoutValueOpts = map[string]bool{"-s": true, "--signal": true, "-k": true, "--kill-after": true}

// watchValueOpts are watch's options that take the next word as value.
var watchValueOpts = map[string]bool{"-n": true, "--interval": true, "-q": true, "--equexit": true}

// destructiveCommand reports a simple command that is a destructive verb,
// a destructive git subcommand, find/xargs, timeout or watch running one, a
// tool's delete/destroy subcommand, rsync --delete, or a shell reading its
// script from stdin.
func destructiveCommand(words []string) bool {
	name, args := shellparse.Command(words)
	if destructiveVerbs[name] || strings.HasPrefix(name, "mkfs") {
		return true
	}
	if shellparse.Shells[name] {
		return shellReadsStdin(args)
	}
	if subs := destructiveSubcommands[name]; subs != nil {
		for _, a := range args {
			if subs[a] {
				return true
			}
		}
		return false
	}
	switch name {
	case "git":
		return destructiveGit(args)
	case "timeout":
		// timeout [options] duration command…
		i := skipOptions(args, timeoutValueOpts)
		return i+1 < len(args) && destructiveCommand(args[i+1:])
	case "watch":
		// watch runs its arguments as one sh -c command line.
		i := skipOptions(args, watchValueOpts)
		return i < len(args) && shellparse.Walk(strings.Join(args[i:], " "), destructiveCommand) != shellparse.None
	case "rsync":
		for _, a := range args {
			if strings.HasPrefix(a, "--delete") || a == "--remove-source-files" {
				return true
			}
		}
	case "find":
		for i, a := range args {
			if a == "-delete" {
				return true
			}
			if (a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir") && i+1 < len(args) {
				if n, _ := shellparse.Command(args[i+1:]); destructiveVerbs[n] {
					return true
				}
			}
		}
	case "xargs":
		// Any destructive verb among xargs' words: its options may take
		// values (-n 1, -I {}), so the command word is not reliably the
		// first non-option.
		for _, a := range args {
			if n := shellparse.CommandName(a); destructiveVerbs[n] || strings.HasPrefix(n, "mkfs") {
				return true
			}
		}
	}
	return false
}

// destructiveGit reports git subcommands that discard work: clean, a
// forced or deleting push, reset --hard, checkout of paths (--, .) or forced, restore
// of the working tree, branch -D, and stash drop/clear.
func destructiveGit(args []string) bool {
	sub, i := "", 0
	for ; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace":
			i++ // a global option whose value is the next word
		case !strings.HasPrefix(a, "-"):
			sub = a
		}
		if sub != "" {
			break
		}
	}
	if sub == "" {
		return false
	}
	rest := args[i+1:]
	switch sub {
	case "clean":
		return true
	case "push":
		// A forced push, a deletion of remote refs (--delete, -d, :ref,
		// --mirror, --prune) or a forced refspec (+ref).
		for _, f := range rest {
			if f == "--force" || f == "--force-with-lease" || strings.HasPrefix(f, "--force=") ||
				strings.HasPrefix(f, "--force-with-lease=") ||
				f == "--delete" || f == "--mirror" || f == "--prune" ||
				shortFlag(f, 'f') || shortFlag(f, 'd') ||
				strings.HasPrefix(f, "+") || strings.HasPrefix(f, ":") {
				return true
			}
		}
	case "reset":
		return contains(rest, "--hard")
	case "checkout":
		for _, f := range rest {
			if f == "--" || f == "." || f == "-f" || f == "--force" {
				return true
			}
		}
	case "restore":
		// --staged alone only unstages; any other restore rewrites the
		// working tree.
		staged := contains(rest, "--staged") || contains(rest, "-S")
		worktree := contains(rest, "--worktree") || contains(rest, "-W")
		return !staged || worktree
	case "branch":
		// -D, or a delete (-d, --delete) that is forced (-f, --force), in
		// separate or combined flags (-fd).
		del, force := false, false
		for _, f := range rest {
			if shortFlag(f, 'D') {
				return true
			}
			del = del || f == "--delete" || shortFlag(f, 'd')
			force = force || f == "--force" || shortFlag(f, 'f')
		}
		return del && force
	case "stash":
		return len(rest) > 0 && (rest[0] == "drop" || rest[0] == "clear")
	}
	return false
}

// shellReadsStdin reports a shell running a script nobody can see: one
// reading it from stdin (curl … | sh, bash -s, bash < file, bash <file,
// bash 0<file, a heredoc or here-string, or no script at all) or from a
// process substitution (bash <(curl …)). An option's value (-o pipefail,
// --rcfile file) is not a script, nor is an output redirect's target. A
// shell given -c or a script file was walked already.
func shellReadsStdin(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-s" || strings.HasPrefix(a, "<("):
			return true
		case shellparse.IsRedirect(a):
			if strings.HasPrefix(strings.TrimLeft(a, "0123456789&"), "<") {
				return true // input: <, <file, 0<file, <<EOF, <<<
			}
			if shellparse.RedirectTakesNext(a) {
				i++ // > file: the target is not a script
			}
		case a == "--":
			continue
		case shellparse.ShellValueOptions[a]:
			i++
		case !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+"):
			return false // a script file
		case a == "-c" || !strings.HasPrefix(a, "--") && strings.Contains(a[1:], "c"):
			return false
		}
	}
	return true
}

// skipOptions returns the index of the first word in args that is not an
// option, stepping over the value of an option in valueOpts and a "--".
func skipOptions(args []string, valueOpts map[string]bool) int {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if args[i] == "--" {
			return i + 1
		}
		if valueOpts[args[i]] {
			i++
		}
		i++
	}
	return i
}

// shortFlag reports a single-dash flag group (-fd) that holds letter c.
func shortFlag(a string, c rune) bool {
	return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a[1:], c)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
