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
	return Walk(command, RmRefused)
}

// RmRefused reports whether a simple command (a word list) holds an rm the
// bash tool refuses: any word that resolves to rm, with the words after it
// as its arguments (rm -r rm / removes /). It reads every rm's arguments
// in one backward pass, so a line of many rm words stays linear.
func RmRefused(words []string) bool {
	// sum[e][k] summarises rm's reading of words[k:], entered with
	// end-of-flags off (e=0) or on (e=1), as RmFlags would read it.
	type summary struct{ recursive, force, critical, sysOrHome bool }
	n := len(words)
	var c int64
	defer func() { count(c) }()
	var sum [2][]summary
	sum[0], sum[1] = make([]summary, n+2), make([]summary, n+2)
	merge := func(x, y summary) summary {
		return summary{x.recursive || y.recursive, x.force || y.force, x.critical || y.critical, x.sysOrHome || y.sysOrHome}
	}
	target := func(t string) summary { return summary{critical: criticalPath(t), sysOrHome: systemOrHomePath(t)} }
	for k := n - 1; k >= 0; k-- {
		c++
		a := words[k]
		sum[1][k] = merge(target(a), sum[1][k+1])
		switch {
		case IsRedirect(a):
			next := k + 1
			if RedirectTakesNext(a) {
				next = min(k+2, n)
			}
			sum[0][k] = sum[0][next]
		case a == "--":
			sum[0][k] = sum[1][k+1]
		case strings.HasPrefix(a, "--"):
			sum[0][k] = merge(summary{recursive: strings.HasPrefix("--recursive", a), force: strings.HasPrefix("--force", a)}, sum[0][k+1])
		case strings.HasPrefix(a, "-") && len(a) > 1:
			sum[0][k] = merge(summary{recursive: strings.ContainsAny(a, "rR"), force: strings.Contains(a, "f")}, sum[0][k+1])
		default:
			sum[0][k] = merge(target(a), sum[0][k+1])
		}
	}
	for i, w := range words {
		c++
		if CommandName(w) == "rm" {
			if s := sum[0][i+1]; s.recursive && (s.critical || s.force && s.sysOrHome) {
				return true
			}
		}
	}
	return false
}

// RmFlags reads rm's arguments: recursive (-r, -R, --recursive or a GNU
// prefix of it such as --rec), force (-f, --force or a prefix such as
// --for) and the paths it removes. Redirections are not paths.
func RmFlags(args []string) (recursive, force bool, targets []string) {
	endOfFlags := false
	var c int64
	defer func() { count(c) }()
	for k := 0; k < len(args); k++ {
		c++
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
