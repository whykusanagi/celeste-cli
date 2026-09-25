package compact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// SummarizeFunc sends a system and a user prompt to a model (the small-model
// role) and returns its text reply.
type SummarizeFunc func(ctx context.Context, system, user string) (string, error)

const (
	// keepTokens is how much of the newest history a summary keeps verbatim.
	keepTokens = 20_000
	// maxTranscriptBytes bounds what the summarizer reads.
	maxTranscriptBytes = 240_000
	// maxToolBodyBytes bounds each tool result in the transcript.
	maxToolBodyBytes = 2_000

	summaryOpen  = "<compacted-context>"
	summaryClose = "</compacted-context>"
)

// SummarySystemPrompt is the structured template the summarizer fills in.
// The goal is restated from the original request because it is the one
// thing a chain of summaries most easily drifts from.
const SummarySystemPrompt = `You compact a coding session so work can continue in a fresh context window.
Write a summary with exactly these sections, in this order, as Markdown headings:

## Goal
The user's ORIGINAL request, restated precisely (not the latest sub-task).
## Constraints & preferences
Requirements, preferences and rules the user stated.
## Progress
### Done
### In progress
### Blocked
## Key decisions
Decisions made and why.
## Critical context
Facts needed to continue: error messages, commands, names, values, findings.
## Files
Files read and files modified, with one line on each.
## Next step
The single next action.

Be specific and factual; keep paths, identifiers and error text exact. If a previous summary is given, merge it: keep what is still true, update what changed. Do not address the user.`

// SummaryOptions controls a summary.
type SummaryOptions struct {
	// KeepTokens of the newest history stay verbatim (default 20k).
	KeepTokens int
	// Focus is an optional instruction from /compact [focus].
	Focus string
	// All summarizes the whole history, keeping no tail (/handoff).
	All bool
}

// SummaryResult describes a summary.
type SummaryResult struct {
	// Cut is how many leading messages the summary replaced.
	Cut           int
	Summary       string
	TokensBefore  int
	TokensAfter   int
	HadPrevious   bool
	MessagesAfter int
}

// Line is a one-line description for logs and the UI.
func (r SummaryResult) Line() string {
	return fmt.Sprintf("summarized %d messages (~%d → ~%d tokens)", r.Cut, r.TokensBefore, r.TokensAfter)
}

// ErrNothingToSummarize means the history is all inside the kept tail, or
// the part before it is too small for a summary to shrink.
var ErrNothingToSummarize = errors.New("nothing to summarize: the older history is too small to shrink")

// CutIndex returns where the kept tail starts: the newest keep tokens,
// extended back so the tail never starts with a tool result (a tool result
// must follow the assistant turn that called it).
func CutIndex(msgs []tui.ChatMessage, keep int) int {
	if keep <= 0 {
		keep = keepTokens
	}
	cut := protectedBoundary(msgs, keep)
	if cut >= len(msgs) {
		// Even the newest message is over keep: still keep the latest turn.
		cut = len(msgs) - 1
	}
	for cut > 0 && msgs[cut].Role == "tool" {
		cut--
	}
	return cut
}

// Summarize replaces the history before the kept tail with a structured
// summary written by summarize. A previous summary at the head is fed back
// in, so summaries accumulate instead of losing earlier work.
func Summarize(ctx context.Context, msgs []tui.ChatMessage, opts SummaryOptions, summarize SummarizeFunc) ([]tui.ChatMessage, SummaryResult, error) {
	if summarize == nil {
		return msgs, SummaryResult{}, errors.New("no summarizer configured")
	}
	cut := len(msgs)
	if !opts.All {
		cut = CutIndex(msgs, opts.KeepTokens)
	}
	if cut <= 0 {
		return msgs, SummaryResult{}, ErrNothingToSummarize
	}
	head, tail := msgs[:cut], msgs[cut:]

	prompt, hadPrevious := summaryPrompt(head, opts.Focus)
	text, err := summarize(ctx, SummarySystemPrompt, prompt)
	if err != nil {
		return msgs, SummaryResult{}, fmt.Errorf("summarize: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return msgs, SummaryResult{}, errors.New("summarize: the model returned an empty summary")
	}

	out := SummaryMessages(text, len(tail) > 0 && tail[0].Role == "user")
	out = append(out, tail...)
	res := SummaryResult{
		Cut:           cut,
		Summary:       text,
		TokensBefore:  Estimate(msgs),
		TokensAfter:   Estimate(out),
		HadPrevious:   hadPrevious,
		MessagesAfter: len(out),
	}
	if !opts.All && res.TokensAfter >= res.TokensBefore {
		// Only a small head was old enough to cut; replacing it would grow the context.
		return msgs, SummaryResult{}, ErrNothingToSummarize
	}
	return out, res, nil
}

// SummaryMessages builds the messages that stand in for the summarized
// history: a user message carrying the summary (not a system message, which
// some providers drop mid-conversation) and, when the kept tail starts with
// a user turn, an assistant acknowledgement so roles still alternate.
func SummaryMessages(summary string, tailStartsWithUser bool) []tui.ChatMessage {
	now := time.Now()
	out := []tui.ChatMessage{{
		Role: "user",
		Content: summaryOpen + "\n" + summary + "\n" + summaryClose +
			"\nThe earlier part of this conversation was compacted into the summary above. Continue the work from it.",
		Timestamp: now,
	}}
	if tailStartsWithUser {
		out = append(out, tui.ChatMessage{Role: "assistant", Content: "Understood. Continuing from the summary.", Timestamp: now})
	}
	return out
}

// HandoffText is the opening message of a session started with /handoff:
// the summary of the previous one, for the user to edit and send.
func HandoffText(summary string) string {
	return "Continuing work from a previous session. Handoff notes:\n\n" + strings.TrimSpace(summary) +
		"\n\nPick up from the next steps above."
}

// IsSummary reports whether a message carries a compaction summary.
func IsSummary(m tui.ChatMessage) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, summaryOpen)
}

// summaryPrompt renders the head of the conversation for the summarizer.
func summaryPrompt(head []tui.ChatMessage, focus string) (string, bool) {
	var b strings.Builder
	previous := ""
	start := 0
	if len(head) > 0 && IsSummary(head[0]) {
		body := strings.TrimPrefix(head[0].Content, summaryOpen)
		if i := strings.Index(body, summaryClose); i >= 0 {
			body = body[:i]
		}
		previous = strings.TrimSpace(body)
		start = 1
		if start < len(head) && head[start].Role == "assistant" && len(head[start].ToolCalls) == 0 {
			start++ // the acknowledgement
		}
	}
	if previous != "" {
		b.WriteString("Previous summary (merge it; the original goal is in it):\n")
		b.WriteString(previous)
		b.WriteString("\n\n")
	}
	if focus = strings.TrimSpace(focus); focus != "" {
		fmt.Fprintf(&b, "The user asked the summary to focus on: %s\n\n", focus)
	}

	var t strings.Builder
	for _, m := range head[start:] {
		switch {
		case m.Role == "tool":
			body := m.Content
			if len(body) > maxToolBodyBytes {
				body = body[:maxToolBodyBytes] + fmt.Sprintf(" …[%d more bytes]", len(m.Content)-maxToolBodyBytes)
			}
			fmt.Fprintf(&t, "[tool result: %s]\n%s\n\n", m.Name, body)
		case len(m.ToolCalls) > 0:
			if strings.TrimSpace(m.Content) != "" {
				fmt.Fprintf(&t, "[assistant]\n%s\n", m.Content)
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if len(args) > 500 {
					args = args[:500] + "…"
				}
				fmt.Fprintf(&t, "[assistant calls %s] %s\n", tc.Name, args)
			}
			t.WriteString("\n")
		default:
			fmt.Fprintf(&t, "[%s]\n%s\n\n", m.Role, m.Content)
		}
	}
	transcript := t.String()
	if len(transcript) > maxTranscriptBytes {
		// Keep the start (the original request) and the most recent part.
		half := maxTranscriptBytes / 2
		transcript = transcript[:half] + "\n…[middle of the transcript omitted]…\n" + transcript[len(transcript)-half:]
	}
	b.WriteString("Conversation to summarize:\n\n")
	b.WriteString(transcript)
	return b.String(), previous != ""
}
