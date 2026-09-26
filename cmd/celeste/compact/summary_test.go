package compact

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// fakeSummarizer records what it was asked and returns a canned summary.
type fakeSummarizer struct {
	system, user string
	reply        string
	err          error
}

func (f *fakeSummarizer) fn(_ context.Context, system, user string) (string, error) {
	f.system, f.user = system, user
	return f.reply, f.err
}

func TestSummarizeReplacesHeadAndKeepsTail(t *testing.T) {
	msgs := history(
		step{"read_file", `{"path":"a.go"}`, 40_000},
		step{"read_file", `{"path":"b.go"}`, 40_000},
		step{"read_file", `{"path":"c.go"}`, 40_000},
	)
	msgs[0].Content = "Refactor the parser to stream tokens"
	f := &fakeSummarizer{reply: "## Goal\nRefactor the parser"}

	out, res, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 12_000}, f.fn)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSummary(out[0]) || !strings.Contains(out[0].Content, "Refactor the parser") {
		t.Fatalf("first message should carry the summary: %+v", out[0])
	}
	// The kept tail starts at an assistant turn, so no acknowledgement.
	if out[1].Role != "assistant" || len(out[1].ToolCalls) == 0 {
		t.Errorf("kept tail should start with the assistant turn that called the tool, got %+v", out[1])
	}
	for i := 1; i < len(out); i++ {
		if out[i].Role == "tool" && (out[i-1].Role != "assistant" && out[i-1].Role != "tool") {
			t.Errorf("tool result at %d is detached from its call", i)
		}
	}
	if res.TokensAfter >= res.TokensBefore {
		t.Errorf("summary did not shrink the history: %d -> %d", res.TokensBefore, res.TokensAfter)
	}
	if !strings.Contains(f.user, "Refactor the parser to stream tokens") {
		t.Error("the original request must reach the summarizer")
	}
	if !strings.Contains(f.system, "ORIGINAL request") {
		t.Error("the structured template should be the system prompt")
	}
}

// The tail never starts with a tool result.
func TestCutIndexNeverStartsWithToolResult(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 8_000})
	for keep := 100; keep < 30_000; keep += 700 {
		if cut := CutIndex(msgs, keep); cut > 0 && msgs[cut].Role == "tool" {
			t.Fatalf("keep=%d: cut at a tool result (%d)", keep, cut)
		}
	}
}

// Summaries are iterative: a previous summary is fed back in and replaced.
func TestSummarizeMergesPreviousSummary(t *testing.T) {
	msgs := append(SummaryMessages("## Goal\nShip the parser rewrite", true),
		history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000})...)
	f := &fakeSummarizer{reply: "## Goal\nShip the parser rewrite\n## Progress\nread a.go"}

	out, res, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 12_000, Focus: "the lexer"}, f.fn)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HadPrevious || !strings.Contains(f.user, "Ship the parser rewrite") {
		t.Error("the previous summary should be fed to the summarizer")
	}
	if !strings.Contains(f.user, "focus on: the lexer") {
		t.Error("the /compact focus should reach the summarizer")
	}
	summaries := 0
	for _, m := range out {
		if IsSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Errorf("want exactly one summary message after merging, got %d", summaries)
	}
}

func TestSummarizeFailuresLeaveHistory(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000})
	for name, f := range map[string]*fakeSummarizer{
		"error": {err: errors.New("boom")},
		"empty": {reply: "  "},
	} {
		out, _, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 12_000}, f.fn)
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(out) != len(msgs) {
			t.Errorf("%s: history changed on failure", name)
		}
	}
	short := []tui.ChatMessage{{Role: "user", Content: "hi"}}
	if _, _, err := Summarize(context.Background(), short, SummaryOptions{}, (&fakeSummarizer{reply: "x"}).fn); !errors.Is(err, ErrNothingToSummarize) {
		t.Errorf("a short history has nothing to summarize: %v", err)
	}
}

// When the kept tail starts with a user turn, an acknowledgement keeps roles
// alternating.
func TestSummaryMessagesAlternate(t *testing.T) {
	if got := SummaryMessages("s", true); len(got) != 2 || got[1].Role != "assistant" {
		t.Errorf("want summary + acknowledgement, got %+v", got)
	}
	if got := SummaryMessages("s", false); len(got) != 1 {
		t.Errorf("want summary only, got %+v", got)
	}
}

// All summarizes the whole history, keeping no tail (/handoff).
func TestSummarizeAllKeepsNoTail(t *testing.T) {
	msgs := []tui.ChatMessage{
		{Role: "user", Content: "fix the parser"},
		{Role: "assistant", Content: "fixed"},
	}
	f := &fakeSummarizer{reply: "## Goal\nparser"}
	out, res, err := Summarize(context.Background(), msgs, SummaryOptions{All: true}, f.fn)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cut != len(msgs) || len(out) != 1 {
		t.Fatalf("want the whole history replaced by the summary; cut=%d out=%d", res.Cut, len(out))
	}
	if !strings.Contains(f.user, "fixed") {
		t.Errorf("the newest message should be summarized too: %q", f.user)
	}
	if !strings.Contains(HandoffText(res.Summary), "## Goal\nparser") {
		t.Errorf("handoff text lost the summary")
	}
}

// When only a tiny head is old enough to cut (here, the opening request before
// one large tool batch), the summary is bigger than what it replaces. Applying
// it would grow the context, so it must be refused (smoke test on #198:
// "summarized 1 messages (~26194 → ~26400 tokens)").
func TestSummarizeRefusesSummaryThatDoesNotShrink(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 100_000})
	if cut := CutIndex(msgs, 0); cut != 1 {
		t.Fatalf("setup: cut = %d, want 1 (only the opening request is old)", cut)
	}
	f := &fakeSummarizer{reply: strings.Repeat("summary ", 300)}
	out, _, err := Summarize(context.Background(), msgs, SummaryOptions{}, f.fn)
	if !errors.Is(err, ErrNothingToSummarize) {
		t.Fatalf("err = %v, want ErrNothingToSummarize", err)
	}
	if len(out) != len(msgs) {
		t.Errorf("history changed: %d messages, want %d", len(out), len(msgs))
	}
}
