package builtin

import (
	"regexp"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellparse"
)

// destructivePatterns: raw disk access, recursive deletes of system paths
// and disk formatting. Compiled once (ruling 3).
var destructivePatterns = []struct {
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

// exfilPatterns: commands that could send local data to another host.
var exfilPatterns = []struct {
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

// systemMods: changes to system configuration, services, kernel and users.
var systemMods = []struct {
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

// forkLoopPattern: a fork in an endless loop.
var forkLoopPattern = regexp.MustCompile(`\bfork\b.*\bwhile\b.*\btrue\b`)

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
	for _, p := range destructivePatterns {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}
	// The regexes above stay as a backstop; this reads the command as a
	// shell does, so quoting, \rm, split or long flags, flags after the
	// path, ~ and $HOME, and nested sh -c / eval / $( ) do not slip past.
	switch shellparse.DestructiveRm(command) {
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
	for _, p := range exfilPatterns {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}

	// === SYSTEM MODIFICATION ===
	for _, p := range systemMods {
		if p.pattern.MatchString(command) {
			return p.reason
		}
	}

	// === FORK BOMBS / RESOURCE EXHAUSTION ===
	if strings.Contains(command, ":(){ :|:& };:") ||
		strings.Contains(command, "./$0|./$0&") ||
		forkLoopPattern.MatchString(lower) {
		return "fork bombs are not permitted"
	}

	return ""
}
