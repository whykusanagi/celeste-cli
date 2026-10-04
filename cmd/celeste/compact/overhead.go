package compact

import "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"

// The fixed prefix of a request (system prompt, tool schemas) is not
// history: no prune or summary can shrink it. Threshold assumes it is a
// small part of the window. On a small window it is not: at 32k the old
// threshold (16k) sat under a ~16k prefix, so every request was "over"
// before any history was sent and every turn ended with a summary that had
// nothing to summarize (local smoke L2). These helpers decide on the
// history beyond the prefix instead.

// minSummaryTokens is the least new history (not counting a previous
// summary) a summary must replace: less than that and the structured
// summary is about as big as what it replaces.
const minSummaryTokens = 2_000

// replyReserve is the room HistoryBudget keeps for the reply: an eighth of
// the window, at most reserveTokens.
func replyReserve(window int) int { return min(window/8, reserveTokens) }

// HistoryBudget is the room left for history on window when every request
// carries overhead fixed tokens and the reply needs replyReserve; 0 when
// the prefix leaves none.
func HistoryBudget(window, overhead int) int {
	return max(window-overhead-replyReserve(window), 0)
}

// ThresholdFor is Threshold for requests that carry overhead fixed tokens.
// When the prefix crowds the window, the threshold rises so the history
// gets three quarters of HistoryBudget before compaction runs, but by no
// more than the overhead: with no prefix, and on large windows where the
// prefix fits, it is Threshold (200k with a 16k prefix: 180k as before).
func ThresholdFor(window, overhead int) int {
	t := Threshold(window)
	if window <= 0 || overhead <= 0 {
		return t
	}
	return max(t, min(overhead+HistoryBudget(window, overhead)*3/4, t+overhead))
}

// KeepWithin is how much history a summary keeps on window when requests
// carry overhead fixed tokens: KeepFor(window), or a third of HistoryBudget
// when that is smaller, so a summary leaves the history well under
// ThresholdFor and the next one is turns away. At least 1 token (the
// newest turn); an unknown window or no prefix keeps KeepFor(window).
func KeepWithin(window, overhead int) int {
	keep := KeepFor(window)
	if window <= 0 || overhead <= 0 {
		return keep
	}
	return max(min(keep, HistoryBudget(window, overhead)/3), 1)
}

// NeedsSummary reports whether a proactive summary is due: the next
// request (used tokens: history plus fixed prefix, as Meter.Used counts
// it) is over ThresholdFor, the prefix leaves the history some room, and
// there is history a summary can replace (summarizable). It never says yes
// when the summary would come back ErrNothingToSummarize for want of
// history, so a chat is not summarized, then "skipped", every turn.
func NeedsSummary(msgs []tui.ChatMessage, window, used int) bool {
	overhead := max(used-Estimate(msgs), 0)
	if window <= 0 || used <= ThresholdFor(window, overhead) || HistoryBudget(window, overhead) <= 0 {
		return false
	}
	return summarizable(msgs, KeepWithin(window, overhead), min(minSummaryTokens, window/16))
}

// summarizable reports whether the history before the kept tail (keep
// tokens, as Summarize cuts it) holds at least minTokens besides a previous
// summary and its acknowledgement.
func summarizable(msgs []tui.ChatMessage, keep, minTokens int) bool {
	if len(msgs) == 0 {
		return false
	}
	cut := CutIndex(msgs, keep)
	if cut <= 0 {
		return false
	}
	head := msgs[:cut]
	if IsSummary(head[0]) {
		head = head[1:]
		if len(head) > 0 && head[0].Role == "assistant" && len(head[0].ToolCalls) == 0 {
			head = head[1:]
		}
	}
	return Estimate(head) >= minTokens
}
