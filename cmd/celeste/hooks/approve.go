package hooks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// DescribeSource prints a source's hooks as the user must see them before
// approving. Every string from the file is shown Go-quoted, so control and
// bidi characters appear as escapes instead of acting on the terminal.
func DescribeSource(w io.Writer, src Source) {
	if src.Kind == KindRepoMCP {
		// The summary quotes every string from the file already; quote a
		// line again only if SafeText would.
		for _, line := range strings.Split(src.Rules, "\n") {
			fmt.Fprintf(w, "    %s\n", SafeText(line))
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

// Decide is the one trust step every repo source passes (hooks, stream
// rules, sandbox settings, MCP servers): run reports whether src may run,
// why says why not ("declined", "not trusted", ...). A Trusted source runs
// and a Declined one does not, without asking; any other is put to approve
// (nil in non-interactive runs: it then never runs), and a yes or a no is
// recorded. err is a failure to record the answer: a yes then holds for
// this session only, a no still skips the source.
func Decide(store *TrustStore, src Source, approve ApproveFunc) (run bool, why string, err error) {
	status := store.Status(src)
	switch status {
	case Trusted:
		return true, "", nil
	case Declined:
		return false, "declined", nil
	}
	if approve != nil {
		switch approve(src, status) {
		case AnswerYes:
			if err := store.Approve(src); err != nil {
				return true, "", fmt.Errorf("%s approved for this session only: %w", strconv.Quote(src.Path), err)
			}
			return true, "", nil
		case AnswerNo:
			if err := store.Decline(src); err != nil {
				return false, "declined", fmt.Errorf("%s declined for this session only: %w", strconv.Quote(src.Path), err)
			}
			return false, "declined", nil
		}
	}
	switch status {
	case Changed:
		return false, "changed since you approved it", nil
	case DeclinedChanged:
		return false, "changed since you declined it", nil
	}
	return false, "not trusted", nil
}

// PromptApprover asks on out and reads a y/N answer from in. y or yes
// approves; anything else on the line (Enter included) declines, and the
// decline is remembered. EOF is no answer (AnswerLater).
func PromptApprover(in io.Reader, out io.Writer) ApproveFunc {
	return promptApprover(in, out, true)
}

// PromptApproverNoRemember is PromptApprover for a caller that records
// only a yes (`celeste hooks trust`): the prompt says a no leaves the
// stored decision as it was instead of claiming it is remembered.
func PromptApproverNoRemember(in io.Reader, out io.Writer) ApproveFunc {
	return promptApprover(in, out, false)
}

func promptApprover(in io.Reader, out io.Writer, rememberNo bool) ApproveFunc {
	reader := bufio.NewReader(in)
	const unchanged = "A no leaves the stored decision unchanged.\n"
	return func(src Source, status TrustStatus) Answer {
		what := "are not trusted yet"
		switch status {
		case Changed:
			what = "have changed since you approved them"
		case DeclinedChanged:
			what = "have changed since you declined them"
		case Declined:
			what = "were declined"
		}
		remembered := "A no is remembered; `celeste hooks trust` approves them later.\n"
		if !rememberNo {
			remembered = unchanged
		}
		switch src.Kind {
		case KindRepoStreamRules:
			fmt.Fprintf(out, "\nStream rules in %s %s:\n", strconv.Quote(strings.TrimSuffix(src.Path, streamRulesSuffix)), what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These rules can stop replies, re-run turns and add instructions the model follows.\n"+remembered+"Trust them? [y/N]: ")
		case KindRepoSandbox:
			fmt.Fprintf(out, "\nSandbox settings in %s %s:\n", strconv.Quote(strings.TrimSuffix(src.Path, sandboxSuffix)), what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These settings loosen the sandbox bash commands run in (more writable directories, the network, or no sandbox).\n"+remembered+"Trust them? [y/N]: ")
		case KindRepoMCP:
			// One server: singular verbs (C4).
			what = "is not trusted yet"
			switch status {
			case Changed:
				what = "has changed since you approved it"
			case DeclinedChanged:
				what = "has changed since you declined it"
			case Declined:
				what = "was declined"
			}
			fmt.Fprintf(out, "\nMCP server %s in %s %s:\n", strconv.Quote(MCPServerName(src)), strconv.Quote(SourceFile(src)), what)
			DescribeSource(out, src)
			mcpRemembered := fmt.Sprintf("A no is remembered; `celeste mcp trust %s` approves it later.\n", SafeText(MCPServerName(src)))
			if !rememberNo {
				mcpRemembered = unchanged
			}
			fmt.Fprint(out, "Starting it runs this command on this machine with your permissions (or connects to this URL).\n"+mcpRemembered+"Trust it? [y/N]: ")
		default:
			fmt.Fprintf(out, "\nHooks in %s (%s) %s:\n", strconv.Quote(src.Path), src.Kind, what)
			DescribeSource(out, src)
			fmt.Fprint(out, "These commands run on this machine with your permissions.\n"+remembered+"Trust them? [y/N]: ")
		}
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return AnswerLater
		}
		if answer := strings.ToLower(strings.TrimSpace(line)); answer == "y" || answer == "yes" {
			return AnswerYes
		}
		return AnswerNo
	}
}

// IsTerminal reports whether f is an interactive terminal. Callers require it
// for both the input and the output of a prompt. Character devices such as
// /dev/null and NUL are not accepted; use `celeste hooks trust` there.
func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
