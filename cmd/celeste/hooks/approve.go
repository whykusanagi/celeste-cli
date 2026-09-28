package hooks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// DescribeSource prints a source's hooks as the user must see them before
// approving. Every string from the file is shown Go-quoted, so control and
// bidi characters appear as escapes instead of acting on the terminal.
func DescribeSource(w io.Writer, src Source) {
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
		fmt.Fprintf(out, "\nHooks in %s (%s) %s:\n", strconv.Quote(src.Path), src.Kind, what)
		DescribeSource(out, src)
		fmt.Fprint(out, "These commands run on this machine with your permissions.\nTrust them? [y/N]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes"
	}
}

// IsTerminal reports whether f is a character device. Callers require it
// for both the input and the output of a prompt. Git Bash/mintty consoles
// are pipes, not terminals; there, `celeste hooks trust` is the way to approve.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
