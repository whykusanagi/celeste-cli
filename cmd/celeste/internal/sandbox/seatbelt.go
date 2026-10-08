package sandbox

import (
	"path/filepath"
	"strings"
)

// sandboxExec is macOS's seatbelt front end, present on every supported
// release.
const sandboxExec = "/usr/bin/sandbox-exec"

// Profile renders p as a seatbelt (SBPL) profile: everything is allowed
// but file writes, which are allowed only under the writable directories
// and to the terminal and pipe devices, never to the read-only paths;
// with the network off, everything but local unix sockets is denied too.
// Later rules win in SBPL, so each allowance follows its deny and the
// read-only paths' deny follows the allowance. That deny also covers each
// read-only path's ancestors themselves (not what is in them), so a
// directory holding one cannot be renamed aside and replaced.
func Profile(p Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	for _, dir := range p.Writable {
		b.WriteString("  (subpath " + sbplQuote(dir) + ")\n")
	}
	b.WriteString("  (literal \"/dev/null\")\n  (literal \"/dev/tty\")\n  (regex #\"^/dev/fd/\"))\n")
	if len(p.ReadOnly) > 0 {
		b.WriteString("(deny file-write*\n")
		seen := map[string]bool{}
		for _, ro := range p.ReadOnly {
			b.WriteString("  (subpath " + sbplQuote(ro) + ")\n")
			for dir := filepath.Dir(ro); !seen[dir]; dir = filepath.Dir(dir) {
				seen[dir] = true
				b.WriteString("  (literal " + sbplQuote(dir) + ")\n")
				if filepath.Dir(dir) == dir {
					break
				}
			}
		}
		b.WriteString(")\n")
	}
	if !p.Network {
		b.WriteString("(deny network*)\n(allow network* (local unix-socket))\n")
	}
	return b.String()
}

// sbplQuote quotes s as an SBPL string literal.
func sbplQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}
