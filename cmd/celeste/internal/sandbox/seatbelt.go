package sandbox

import "strings"

// sandboxExec is macOS's seatbelt front end, present on every supported
// release.
const sandboxExec = "/usr/bin/sandbox-exec"

// Profile renders p as a seatbelt (SBPL) profile: everything is allowed
// but file writes, which are allowed only under the writable directories
// and to the terminal and pipe devices; with the network off, everything
// but local unix sockets is denied too. Later rules win in SBPL, so each
// allowance follows its deny.
func Profile(p Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	for _, dir := range p.Writable {
		b.WriteString("  (subpath " + sbplQuote(dir) + ")\n")
	}
	b.WriteString("  (literal \"/dev/null\")\n  (literal \"/dev/tty\")\n  (regex #\"^/dev/fd/\"))\n")
	if !p.Network {
		b.WriteString("(deny network*)\n(allow network* (local unix-socket))\n")
	}
	return b.String()
}

// sbplQuote quotes s as an SBPL string literal.
func sbplQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}
