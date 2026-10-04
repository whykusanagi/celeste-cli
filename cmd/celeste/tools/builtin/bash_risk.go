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

// destructiveCommand reports a simple command that is a destructive verb,
// a destructive git subcommand, find/xargs running one, or a shell reading
// its script from stdin.
func destructiveCommand(words []string) bool {
	name, args := shellparse.Command(words)
	if destructiveVerbs[name] || strings.HasPrefix(name, "mkfs") {
		return true
	}
	if shellparse.Shells[name] {
		// A shell reading its script from stdin (curl … | sh) runs code
		// nobody can see; one given -c or a script was walked already.
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				return false
			}
		}
		return !contains(args, "-c")
	}
	switch name {
	case "git":
		return destructiveGit(args)
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
// forced push, reset --hard, checkout of paths (--, .) or forced, restore
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
		for _, f := range rest {
			if f == "--force" || f == "--force-with-lease" || strings.HasPrefix(f, "--force=") ||
				(strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.Contains(f, "f")) ||
				strings.HasPrefix(f, "+") {
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
		return contains(rest, "-D") || (contains(rest, "--delete") && contains(rest, "--force"))
	case "stash":
		return len(rest) > 0 && (rest[0] == "drop" || rest[0] == "clear")
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
