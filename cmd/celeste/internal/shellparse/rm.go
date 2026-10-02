package shellparse

import (
	"path"
	"strings"
)

// DestructiveRm reports whether any command in the line, nested ones
// included, is a recursive rm of a system or home path (Found), or whether
// the line nests command strings too deep to check (TooDeep). Every word
// that resolves to rm starts a check of the words after it, so wrappers
// (xargs, timeout 5, nice -n 5) do not hide it. The bash tool refuses what
// it reports; the advisory destructive-bash rule and the watchdog
// (rules.Destructive) see at least the same.
//
// Out of scope (documented, not checked): relative targets that climb out
// with .., paths that come from a variable, a substitution or a glob or
// brace expansion (X=/; rm -rf $X), find -delete, deletes from other
// programs (python shutil.rmtree), chmod/chown -R, commands fed to a shell
// on stdin, and paths that reach rm through stdin (find / | xargs rm -rf)
// or a script.
func DestructiveRm(command string) Result {
	return Walk(command, func(words []string) bool {
		for i, w := range words {
			if CommandName(w) == "rm" && rmDestructive(words[i+1:]) {
				return true
			}
		}
		return false
	})
}

// RmFlags reads rm's arguments: recursive (-r, -R, --recursive or a GNU
// prefix of it such as --rec), force (-f, --force or a prefix such as
// --for) and the paths it removes. Redirections are not paths.
func RmFlags(args []string) (recursive, force bool, targets []string) {
	endOfFlags := false
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case !endOfFlags && IsRedirect(a):
			if RedirectTakesNext(a) {
				k++ // "> file": the target is the next word
			}
		case endOfFlags:
			targets = append(targets, a)
		case a == "--":
			endOfFlags = true
		case strings.HasPrefix(a, "--"):
			// GNU takes any unambiguous prefix: --r.. is only --recursive
			// and --f.. only --force among rm's long options.
			recursive = recursive || strings.HasPrefix("--recursive", a)
			force = force || strings.HasPrefix("--force", a)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			recursive = recursive || strings.ContainsAny(a, "rR")
			force = force || strings.Contains(a, "f")
		default:
			targets = append(targets, a)
		}
	}
	return recursive, force, targets
}

// rmDestructive: rm's arguments ask for a recursive delete of a path that
// matters. A bash tool call runs without a TTY, so rm never prompts and -r
// alone removes as much as -rf: the root, a home directory and any
// top-level directory are refused without -f. Deeper absolute paths are
// refused only with -f.
func rmDestructive(args []string) bool {
	recursive, force, targets := RmFlags(args)
	if !recursive {
		return false
	}
	for _, t := range targets {
		if criticalPath(t) || force && systemOrHomePath(t) {
			return true
		}
	}
	return false
}

// IsRedirect: >, >>, 2>, &>, 2>&1, >file, <file ...
func IsRedirect(w string) bool {
	t := strings.TrimLeft(w, "0123456789&")
	return strings.HasPrefix(t, ">") || strings.HasPrefix(t, "<")
}

// RedirectTakesNext: a bare operator ("> file") names its target in the
// next word; "2>&1" and ">file" do not.
func RedirectTakesNext(w string) bool {
	t := strings.TrimLeft(w, "0123456789&")
	t = strings.TrimLeft(t, "<>")
	return t == "" || t == "|"
}

// criticalPath: the root, a home directory itself, or a top-level
// directory (/usr, /etc, /*): what rm -r alone must never get.
func criticalPath(t string) bool {
	if strings.HasPrefix(t, "/") {
		return strings.Count(path.Clean(t), "/") <= 1
	}
	return systemOrHomePath(t)
}

// systemOrHomePath: any absolute path, or a home directory itself (~,
// ~user, $HOME, ${HOME}, with or without a trailing / or /*) or a path
// that climbs out of it (~/..). Paths below home (~/project/build) are not.
func systemOrHomePath(t string) bool {
	if strings.HasPrefix(t, "/") {
		return true
	}
	var rest string
	switch {
	case strings.HasPrefix(t, "${HOME}"):
		rest = strings.TrimPrefix(t, "${HOME}")
	case strings.HasPrefix(t, "$HOME"):
		rest = strings.TrimPrefix(t, "$HOME")
	case strings.HasPrefix(t, "~"):
		// ~ or ~user: the user name runs to the first slash.
		if i := strings.IndexByte(t, '/'); i >= 0 {
			rest = t[i:]
		}
	default:
		return false
	}
	rest = strings.TrimSuffix(rest, "*")
	return path.Clean("/"+rest) == "/"
}
