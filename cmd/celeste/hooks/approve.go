package hooks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/term"
)

// DescribeSource prints a source's hooks as the user must see them before
// approving. Every string from the file is shown Go-quoted, so control and
// bidi characters appear as escapes instead of acting on the terminal.
func DescribeSource(w io.Writer, src Source) {
	if src.Kind == KindRepoMCP {
		// The summary quotes every string from the file already; quote a
		// line again only if it still holds a non-printable character.
		for _, line := range strings.Split(src.Rules, "\n") {
			if strings.IndexFunc(line, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
				line = strconv.Quote(line)
			}
			fmt.Fprintf(w, "    %s\n", line)
		}
		return
	}
	if src.Kind == KindRepoStreamRules || src.Kind == KindRepoSandbox {
		for _, line := range strings.Split(src.Rules, "\n") {
			fmt.Fprintf(w, "    %s\n", strconv.Quote(line))
		}
		return
	}
	for _, d := range src.Hooks {
		fmt.Fprintf(w, "  %-16s matcher=%s protocol=%s timeout=%ds\n    command: %s\n",
			d.Event, strconv.Quote(d.Matcher), d.Protocol, d.Timeout, strconv.Quote(d.Command))
	}
}

// PromptApprover asks on out and reads a y/N answer from in. Anything but
// y or yes (including Enter and EOF) declines.
func PromptApprover(in io.Reader, out io.Writer) ApproveFunc {
	reader := bufio.NewReader(in)
	return func(src Source, status TrustStatus) bool {
		what := "are not trusted yet"
		if status == Changed {
			what = "have changed since you approved them"
		}
		switch src.Kind {
		case KindRepoStreamRules:
			fmt.Fprintf(out, "\nStream rules in %s %s:\n", strconv.Quote(strings.TrimSuffix(src.Path, streamRulesSuffix)), what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These rules can stop replies, re-run turns and add instructions the model follows.\nTrust them? [y/N]: ")
		case KindRepoSandbox:
			fmt.Fprintf(out, "\nSandbox settings in %s %s:\n", strconv.Quote(strings.TrimSuffix(src.Path, sandboxSuffix)), what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These settings loosen the sandbox bash commands run in (more writable directories, the network, or no sandbox).\nTrust them? [y/N]: ")
		case KindRepoMCP:
			fmt.Fprintf(out, "\nMCP server %s in %s %s:\n", strconv.Quote(MCPServerName(src)), strconv.Quote(SourceFile(src)), what)
			DescribeSource(out, src)
			fmt.Fprint(out, "Starting it runs this command on this machine with your permissions (or connects to this URL).\nTrust it? [y/N]: ")
		default:
			fmt.Fprintf(out, "\nHooks in %s (%s) %s:\n", strconv.Quote(src.Path), src.Kind, what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These commands run on this machine with your permissions.\nTrust them? [y/N]: ")
		}
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes"
	}
}

// IsTerminal reports whether f is an interactive terminal. Callers require it
// for both the input and the output of a prompt. Character devices such as
// /dev/null and NUL are not accepted; use `celeste hooks trust` there.
func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
