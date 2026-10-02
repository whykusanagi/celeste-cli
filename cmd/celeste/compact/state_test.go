package compact

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestRenderStateIsDeterministic(t *testing.T) {
	todos := []Todo{{ID: 3, Title: "ship it", Status: "pending"}, {ID: 1, Title: "write tests", Status: "done"}, {ID: 2, Title: "fix parser", Status: "in_progress"}}
	files := []string{"cmd/b.go", "README.md", "cmd/a.go", "cmd/a.go"}
	got := RenderState(todos, files, "Voice Boundary:\nplain prose in files")
	want := "## Authoritative state\n" +
		"From celeste's own records; where the summary above disagrees, this is correct.\n\n" +
		"### Todo list\n" +
		"- [x] 1. write tests (done)\n" +
		"- [~] 2. fix parser (in progress)\n" +
		"- [ ] 3. ship it (pending)\n\n" +
		"### Files modified this session\n" +
		"- README.md\n" +
		"- cmd/a.go\n" +
		"- cmd/b.go\n\n" +
		"### Voice rule\n" +
		"Voice Boundary:\nplain prose in files\n"
	if got != want {
		t.Fatalf("RenderState =\n%s\nwant\n%s", got, want)
	}
	// Same state in another order: same bytes.
	again := RenderState([]Todo{todos[2], todos[0], todos[1]}, []string{"cmd/a.go", "cmd/b.go", "README.md"}, "Voice Boundary:\nplain prose in files")
	if again != got {
		t.Fatal("RenderState depends on input order")
	}
}

func TestRenderStateOmitsEmptySections(t *testing.T) {
	if got := RenderState(nil, nil, ""); got != "" {
		t.Fatalf("empty state = %q, want \"\"", got)
	}
	got := RenderState(nil, []string{"a.go"}, "")
	if strings.Contains(got, "Todo list") || strings.Contains(got, "Voice rule") || !strings.Contains(got, "- a.go") {
		t.Fatalf("only the files section expected:\n%s", got)
	}
}

// The spec's #200 test: the summarizer forgets the todos and the files.
func TestSummaryCarriesStateTheSummarizerOmitted(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000}, step{"read_file", `{"path":"c.go"}`, 40_000})
	f := &fakeSummarizer{reply: "## Goal\ndo the task\n## Files\n(none recorded)"}
	state := RenderState([]Todo{{ID: 1, Title: "port the lexer", Status: "in_progress"}}, []string{"lexer.go"}, "")
	out, _, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 12_000, State: state}, f.fn)
	if err != nil {
		t.Fatal(err)
	}
	head := out[0].Content
	if !IsSummary(out[0]) || !strings.Contains(head, "port the lexer (in progress)") || !strings.Contains(head, "- lexer.go") {
		t.Fatalf("summary message lacks the state:\n%s", head)
	}
	if i, j := strings.Index(head, "## Authoritative state"), strings.Index(head, "</compacted-context>"); i < 0 || j < i {
		t.Fatal("the state must sit inside <compacted-context>")
	}
}

// A merged previous summary is fed back without its old state: the state
// is re-rendered from the records each time, never summarized.
func TestPreviousStateIsNotFedToTheSummarizer(t *testing.T) {
	prev := SummaryMessages("## Goal\nold goal", RenderState([]Todo{{ID: 9, Title: "stale item", Status: "pending"}}, nil, ""), false)
	msgs := append(prev, history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000})...)
	f := &fakeSummarizer{reply: "## Goal\nold goal"}
	if _, _, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 8_000}, f.fn); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.user, "stale item") {
		t.Fatal("the previous state reached the summarizer")
	}
	if !strings.Contains(f.user, "old goal") {
		t.Fatal("the previous summary itself must still be merged")
	}
}

// The summarizer may echo the state heading in its own prose; only the
// block SummaryText appended is cut, so the prose after the echo survives.
func TestPreviousSummaryEchoingTheStateHeadingIsKept(t *testing.T) {
	summary := "## Goal\nold goal\n\n## Authoritative state\nthe user said this heading wins\n\n## Next Steps\nkeep porting"
	prev := SummaryMessages(summary, RenderState([]Todo{{ID: 9, Title: "stale item", Status: "pending"}}, nil, ""), false)
	msgs := append(prev, history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000})...)
	f := &fakeSummarizer{reply: "## Goal\nold goal"}
	if _, _, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 8_000}, f.fn); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.user, "keep porting") {
		t.Fatal("the previous summary was cut at the heading its own prose echoed")
	}
	if strings.Contains(f.user, "stale item") {
		t.Fatal("the previous state reached the summarizer")
	}
}

// A state-only message (a server compaction's) feeds no state back either.
func TestPreviousStateMessageIsNotFedToTheSummarizer(t *testing.T) {
	prev := []tui.ChatMessage{StateMessage(RenderState([]Todo{{ID: 9, Title: "stale item", Status: "pending"}}, nil, ""))}
	msgs := append(prev, history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 40_000})...)
	f := &fakeSummarizer{reply: "## Goal\nx"}
	if _, _, err := Summarize(context.Background(), msgs, SummaryOptions{KeepTokens: 8_000}, f.fn); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.user, "stale item") {
		t.Fatal("the previous state reached the summarizer")
	}
}

func TestStateMessage(t *testing.T) {
	m := StateMessage("## Authoritative state\nx\n")
	if m.Role != "user" || !strings.HasPrefix(m.Content, "<compacted-context>") || !strings.Contains(m.Content, "## Authoritative state") {
		t.Fatalf("StateMessage = %+v", m)
	}
}
