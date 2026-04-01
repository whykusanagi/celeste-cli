package ctxmgr

import (
	"context"
	"fmt"
	"time"
)

// ChatMessage mirrors tui.ChatMessage to avoid a circular import.
// The TUI layer converts between the two at integration boundaries.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	Name       string
	Timestamp  time.Time
}

// CompactionResult describes what a compaction operation did.
type CompactionResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int // Estimated tokens in compacted messages
	TokensAfter    int // Estimated tokens in the summary replacement
	TurnsCompacted int // Number of user-assistant turn pairs summarized
}

// CompactionEngine orchestrates conversation compaction using a Summarizer.
type CompactionEngine struct {
	summarizer Summarizer
	// RecentTurnsToKeep controls how many recent user-assistant turn pairs
	// are protected from compaction. Default: 4.
	RecentTurnsToKeep int
}

// NewCompactionEngine creates a CompactionEngine with the given summarizer.
func NewCompactionEngine(summarizer Summarizer) *CompactionEngine {
	return &CompactionEngine{
		summarizer:        summarizer,
		RecentTurnsToKeep: 4,
	}
}

// CompactReactive performs reactive compaction when the context budget crosses
// the 80% threshold. It summarizes the oldest messages while keeping the most
// recent RecentTurnsToKeep turn pairs intact.
//
// The messages slice should be the full conversation history (excluding system
// prompt, which is sent separately). The budget is used to track compaction
// count and recalculate usage after compaction.
func (ce *CompactionEngine) CompactReactive(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	return ce.compact(ctx, messages, budget)
}

// CompactProactive performs proactive compaction. The logic is identical to
// reactive compaction but is triggered on a turn-count interval rather than
// a usage threshold. The caller is responsible for checking
// budget.ShouldCompactProactive() before calling this.
func (ce *CompactionEngine) CompactProactive(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	return ce.compact(ctx, messages, budget)
}

// compact is the shared compaction implementation.
func (ce *CompactionEngine) compact(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	result := CompactionResult{
		MessagesBefore: len(messages),
	}

	if len(messages) == 0 {
		return messages, result, fmt.Errorf("no messages to compact")
	}

	// Find the split point: protect the last N turn pairs.
	// A "turn pair" is a user message followed by an assistant message.
	// We also protect any system messages at the start.
	keepCount := ce.RecentTurnsToKeep
	if keepCount <= 0 {
		keepCount = 4
	}

	splitIdx := findSplitIndex(messages, keepCount)
	if splitIdx <= 0 {
		return messages, result, fmt.Errorf("not enough messages to compact (need more than %d turn pairs)", keepCount)
	}

	// The messages to summarize are messages[0:splitIdx].
	// The messages to keep are messages[splitIdx:].
	toSummarize := messages[:splitIdx]
	toKeep := messages[splitIdx:]

	// Estimate tokens before compaction
	for _, msg := range toSummarize {
		result.TokensBefore += EstimateTokens(msg.Content) + 4 // +4 for role overhead
	}

	// Count turn pairs being compacted
	for _, msg := range toSummarize {
		if msg.Role == "user" {
			result.TurnsCompacted++
		}
	}

	// Summarize
	summary, err := ce.summarizer.Summarize(ctx, toSummarize)
	if err != nil {
		return messages, result, fmt.Errorf("summarization failed: %w", err)
	}

	// Build the compacted message list
	summaryMsg := ChatMessage{
		Role:      "system",
		Content:   fmt.Sprintf("[Conversation Summary - %d turns compacted]\n\n%s", result.TurnsCompacted, summary),
		Timestamp: time.Now(),
	}

	result.TokensAfter = EstimateTokens(summaryMsg.Content) + 4

	compacted := make([]ChatMessage, 0, 1+len(toKeep))
	compacted = append(compacted, summaryMsg)
	compacted = append(compacted, toKeep...)

	result.MessagesAfter = len(compacted)

	// Update budget
	if budget != nil {
		budget.IncrementCompactCount()
		// Recalculate history tokens: subtract what we removed, add the summary
		saved := result.TokensBefore - result.TokensAfter
		budget.mu.Lock()
		budget.HistoryTokens -= saved
		if budget.HistoryTokens < 0 {
			budget.HistoryTokens = 0
		}
		budget.mu.Unlock()
	}

	return compacted, result, nil
}

// findSplitIndex returns the index that separates "old" messages from the
// most recent keepTurnPairs turn pairs. Messages before the split index
// will be summarized; messages from the split index onward are kept.
func findSplitIndex(messages []ChatMessage, keepTurnPairs int) int {
	// Walk backwards counting user messages (each user message starts a turn pair)
	turnsFound := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			turnsFound++
			if turnsFound >= keepTurnPairs {
				return i
			}
		}
	}
	// Not enough turn pairs to split
	return 0
}

// CompactSnip performs inline truncation of a single string without writing
// to disk. This is a lightweight alternative to CapToolResult when you do not
// need persistent storage of the full result (e.g., for intermediate
// processing steps).
func CompactSnip(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}
	if len(text) <= maxBytes {
		return text
	}

	tailSize := 256
	if tailSize > len(text) {
		tailSize = len(text)
	}

	headSize := maxBytes - tailSize - 60 // 60 bytes for the notice
	if headSize < 128 {
		headSize = 128
	}
	if headSize > len(text) {
		headSize = len(text)
	}

	head := text[:headSize]
	tail := text[len(text)-tailSize:]
	notice := fmt.Sprintf("\n[...snipped %d bytes...]\n", len(text)-headSize-tailSize)

	return head + notice + tail
}

// FormatCompactionResult creates a user-friendly summary of what compaction did.
func FormatCompactionResult(r CompactionResult) string {
	tokensSaved := r.TokensBefore - r.TokensAfter
	if r.TokensBefore == 0 {
		return fmt.Sprintf("Compacted: %d msgs -> %d msgs", r.MessagesBefore, r.MessagesAfter)
	}
	savingsPct := float64(tokensSaved) / float64(r.TokensBefore) * 100
	return fmt.Sprintf(
		"Compacted: %d msgs -> %d msgs (%d turns summarized, saved ~%s tokens, %.0f%% reduction)",
		r.MessagesBefore, r.MessagesAfter,
		r.TurnsCompacted,
		FormatTokenCount(tokensSaved),
		savingsPct,
	)
}
