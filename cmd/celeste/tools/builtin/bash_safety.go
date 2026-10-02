package builtin

import (
	"path"
	"regexp"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/shellparse"
)

// checkDangerousCommand inspects a shell command for dangerous patterns.
// Returns a human-readable rejection reason, or "" if the command is safe.
func checkDangerousCommand(command string) string {
	lower := strings.ToLower(command)
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	// === PRIVILEGE ESCALATION ===
	// Block sudo/su anywhere in the command, not just as first word.
	// Catches: sudo X, bash -c "sudo X", command sudo X, env sudo X
	for _, f := range fields {
		if f == "sudo" || f == "su" || f == "doas" || f == "pkexec" {
			return "privilege escalation (sudo/su/doas) is not permitted"
		}
	}

	// === DESTRUCTIVE FILESYSTEM ===
	destructivePatterns := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		// dd — raw disk/device access
		{regexp.MustCompile(`\bdd\s+.*(?:if=|of=)/dev/`), "dd with device paths is not permitted"},
		{regexp.MustCompile(`\bdd\s+.*of=/`), "dd writing to absolute paths is not permitted"},

		// rm -rf / or dangerous recursive deletes
		{regexp.MustCompile(`\brm\s+-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*\s+/[^.]`), "recursive rm on system paths is not permitted"},
		{regexp.MustCompile(`\brm\s+-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*\s+/[^.]`), "recursive rm on system paths is not permitted"},
		{regexp.MustCompile(`\brm\s+-rf\s+/$`), "rm -rf / is not permitted"},
		{regexp.MustCompile(`\brm\s+-rf\s+/\s`), "rm -rf / is not permitted"},

		// mkfs, fdisk, parted — disk formatting
		{regexp.MustCompile(`\b(?:mkfs|fdisk|parted|wipefs|sgdisk|gdisk)\b`), "disk formatting tools are not permitted"},

		// Direct device access
		{regexp.MustCompile(`(?:>|>>)\s*/dev/(?:sd|nvme|disk|hd|vd)`), "direct device writes are not permitted"},
	}

	for _, p := range destructivePatterns {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}
	// The regexes above stay as a backstop; this reads the command as a
	// shell does, so quoting, \rm, split or long flags, flags after the
	// path, ~ and $HOME, and nested sh -c / eval / $( ) do not slip past.
	switch destructiveRm(command) {
	case shellparse.Found:
		return "recursive rm on system or home paths is not permitted"
	case shellparse.TooDeep:
		return "command nesting too deep to check is not permitted"
	}

	// === SENSITIVE FILE ACCESS ===
	sensitiveFiles := []string{
		"/etc/shadow", "/etc/passwd", "/etc/sudoers",
		"/etc/master.passwd", // macOS
		".ssh/id_", ".ssh/authorized_keys",
		".gnupg/", ".aws/credentials",
		".kube/config",
	}
	for _, f := range sensitiveFiles {
		if strings.Contains(lower, f) {
			return "access to " + f + " is not permitted"
		}
	}

	// === NETWORK EXFILTRATION ===
	// Block commands that could exfiltrate data to external servers.
	// Only block when combined with pipe/redirect patterns that suggest data theft.
	exfilPatterns := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		// curl/wget POSTing local files
		{regexp.MustCompile(`\bcurl\b.*(?:-d\s*@|-F\s*file=@|--data-binary\s*@|--upload-file)`), "uploading local files via curl is not permitted"},
		{regexp.MustCompile(`\bwget\b.*--post-file`), "uploading local files via wget is not permitted"},

		// nc/ncat/netcat sending data
		{regexp.MustCompile(`\b(?:nc|ncat|netcat)\b.*(?:<|/dev/)`), "piping data through netcat is not permitted"},

		// scp/rsync to remote (only block outbound, allow inbound)
		{regexp.MustCompile(`\bscp\b.*\s\S+:`), "scp to remote hosts is not permitted"},
	}

	for _, p := range exfilPatterns {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}

	// === SYSTEM MODIFICATION ===
	systemMods := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		// Modifying system configs
		{regexp.MustCompile(`(?:>|>>)\s*/etc/`), "writing to /etc/ is not permitted"},
		{regexp.MustCompile(`\btee\b.*\s/etc/`), "writing to /etc/ is not permitted"},

		// Cron manipulation
		{regexp.MustCompile(`\bcrontab\s+-[er]`), "crontab modification is not permitted"},

		// Service management (could start malicious services)
		{regexp.MustCompile(`\b(?:systemctl|launchctl)\s+(?:enable|start|restart)\b`), "starting system services is not permitted"},

		// Kernel module loading
		{regexp.MustCompile(`\b(?:insmod|modprobe|rmmod)\b`), "kernel module operations are not permitted"},

		// iptables/firewall
		{regexp.MustCompile(`\b(?:iptables|nft|pfctl)\b`), "firewall modification is not permitted"},

		// User management
		{regexp.MustCompile(`\b(?:useradd|userdel|usermod|groupadd|adduser|deluser|chpasswd|passwd)\b`), "user management is not permitted"},
	}

	for _, p := range systemMods {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}

	// === FORK BOMBS / RESOURCE EXHAUSTION ===
	if strings.Contains(command, ":(){ :|:& };:") ||
		strings.Contains(command, "./$0|./$0&") ||
		regexp.MustCompile(`\bfork\b.*\bwhile\b.*\btrue\b`).MatchString(lower) {
		return "fork bombs are not permitted"
	}

	return ""
}

// destructiveRm reports whether any command in the line, nested ones
// included, is a recursive rm of a system or home path (shellparse.Found),
// or whether the line nests command strings too deep to check
// (shellparse.TooDeep). Every word that resolves to rm starts a check of
// the words after it, so wrappers (xargs, timeout 5, nice -n 5) do not
// hide it.
//
// Out of scope (documented, not checked): relative targets that climb out
// with .., paths that come from a variable, a substitution or a glob or
// brace expansion (X=/; rm -rf $X), find -delete, deletes from other
// programs (python shutil.rmtree), chmod/chown -R, commands fed to a shell
// on stdin, and paths that reach rm through stdin (find / | xargs rm -rf)
// or a script. git push --force is the advisory stream rule's job.
func destructiveRm(command string) shellparse.Result {
	return shellparse.Walk(command, func(words []string) bool {
		for i, w := range words {
			if shellparse.CommandName(w) == "rm" && rmDestructive(words[i+1:]) {
				return true
			}
		}
		return false
	})
}

// rmDestructive: rm's arguments ask for a recursive delete (-r, -R,
// --recursive or a GNU prefix of it such as --rec) of a path that matters.
// A bash tool call runs without a TTY, so rm never prompts and -r alone
// removes as much as -rf: the root, a home directory and any top-level
// directory are refused without -f. Deeper absolute paths are refused only
// with -f (-f, --force or a prefix such as --for), as before.
func rmDestructive(args []string) bool {
	recursive, force := false, false
	var targets []string
	endOfFlags := false
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case !endOfFlags && isShellRedirect(a):
			t := strings.TrimLeft(strings.TrimLeft(a, "0123456789&"), "<>")
			if t == "" || t == "|" {
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

// isShellRedirect: >, >>, 2>, &>, 2>&1, >file, <file ...
func isShellRedirect(w string) bool {
	t := strings.TrimLeft(w, "0123456789&")
	return strings.HasPrefix(t, ">") || strings.HasPrefix(t, "<")
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
