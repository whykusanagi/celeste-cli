package ctxmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockSummarizer implements Summarizer for testing.
type mockSummarizer struct {
	summary string
	err     error
	called  int
}

func (m *mockSummarizer) Summarize(ctx context.Context, messages []ChatMessage) (string, error) {
	m.called++
	return m.summary, m.err
}

func makeConversation(turnPairs int) []ChatMessage {
	var msgs []ChatMessage
	for i := 0; i < turnPairs; i++ {
		msgs = append(msgs, ChatMessage{
			Role:      "user",
			Content:   fmt.Sprintf("User message %d with some content to estimate tokens from", i+1),
			Timestamp: time.Now().Add(time.Duration(i*2) * time.Minute),
		})
		msgs = append(msgs, ChatMessage{
			Role:      "assistant",
			Content:   fmt.Sprintf("Assistant response %d with detailed answer content here", i+1),
			Timestamp: time.Now().Add(time.Duration(i*2+1) * time.Minute),
		})
	}
	return msgs
}

func TestCompactReactive_Basic(t *testing.T) {
	mock := &mockSummarizer{summary: "This is a summary of the conversation."}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 2

	msgs := makeConversation(6) // 12 messages, 6 turn pairs
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	compacted, result, err := engine.CompactReactive(ctx, msgs, budget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.called != 1 {
		t.Errorf("summarizer called %d times, want 1", mock.called)
	}

	// Should have: 1 summary + last 2 turn pairs (4 messages) = 5
	if len(compacted) != 5 {
		t.Errorf("compacted length = %d, want 5", len(compacted))
	}

	// First message should be the summary
	if compacted[0].Role != "system" {
		t.Errorf("first message role = %q, want system", compacted[0].Role)
	}
	if !strings.Contains(compacted[0].Content, "Conversation Summary") {
		t.Error("summary message should contain 'Conversation Summary'")
	}

	// Result should have correct counts
	if result.MessagesBefore != 12 {
		t.Errorf("MessagesBefore = %d, want 12", result.MessagesBefore)
	}
	if result.MessagesAfter != 5 {
		t.Errorf("MessagesAfter = %d, want 5", result.MessagesAfter)
	}
	if result.TurnsCompacted != 4 {
		t.Errorf("TurnsCompacted = %d, want 4", result.TurnsCompacted)
	}

	// Budget should have been updated
	if budget.CompactCount != 1 {
		t.Errorf("CompactCount = %d, want 1", budget.CompactCount)
	}
}

func TestCompactReactive_TooFewMessages(t *testing.T) {
	mock := &mockSummarizer{summary: "summary"}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 4

	// Only 3 turn pairs, need 4 to keep -- nothing to compact
	msgs := makeConversation(3)
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	_, _, err := engine.CompactReactive(ctx, msgs, budget)
	if err == nil {
		t.Error("expected error when too few messages to compact")
	}
}

func TestCompactReactive_SummarizerError(t *testing.T) {
	mock := &mockSummarizer{err: fmt.Errorf("LLM unavailable")}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 2

	msgs := makeConversation(6)
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	returned, _, err := engine.CompactReactive(ctx, msgs, budget)
	if err == nil {
		t.Error("expected error when summarizer fails")
	}
	// Should return original messages unchanged
	if len(returned) != len(msgs) {
		t.Errorf("should return original messages on error, got %d want %d", len(returned), len(msgs))
	}
}

func TestCompactReactive_EmptyMessages(t *testing.T) {
	mock := &mockSummarizer{summary: "summary"}
	engine := NewCompactionEngine(mock)

	ctx := context.Background()
	_, _, err := engine.CompactReactive(ctx, nil, nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestCompactReactive_NilBudget(t *testing.T) {
	mock := &mockSummarizer{summary: "A summary."}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 1

	msgs := makeConversation(4)
	ctx := context.Background()

	// Should not panic with nil budget
	compacted, _, err := engine.CompactReactive(ctx, msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compacted) == 0 {
		t.Error("compacted should not be empty")
	}
}

func TestCompactSnip_UnderLimit(t *testing.T) {
	text := "short text"
	result := CompactSnip(text, 1024)
	if result != text {
		t.Errorf("CompactSnip should not modify text under limit")
	}
}

func TestCompactSnip_OverLimit(t *testing.T) {
	text := strings.Repeat("a", 10000)
	result := CompactSnip(text, 1024)

	if len(result) >= len(text) {
		t.Error("CompactSnip result should be shorter than input")
	}
	if !strings.Contains(result, "snipped") {
		t.Error("CompactSnip result should contain snip notice")
	}
	// Should contain the tail of the original text
	if !strings.HasSuffix(result, strings.Repeat("a", 256)) {
		t.Error("CompactSnip result should end with tail of original")
	}
}

func TestFormatCompactionResult(t *testing.T) {
	r := CompactionResult{
		MessagesBefore: 20,
		MessagesAfter:  6,
		TokensBefore:   5000,
		TokensAfter:    300,
		TurnsCompacted: 7,
	}
	formatted := FormatCompactionResult(r)
	if !strings.Contains(formatted, "20 msgs") {
		t.Error("should contain message count before")
	}
	if !strings.Contains(formatted, "6 msgs") {
		t.Error("should contain message count after")
	}
	if !strings.Contains(formatted, "7 turns") {
		t.Error("should contain turns compacted")
	}
}

func TestFindSplitIndex(t *testing.T) {
	msgs := makeConversation(5) // 10 messages

	// Keep last 2 turn pairs -- split should be at index 6 (user message of 4th pair)
	idx := findSplitIndex(msgs, 2)
	if idx != 6 {
		t.Errorf("findSplitIndex(keep=2) = %d, want 6", idx)
	}

	// Keep last 5 -- all messages are recent, split at 0
	idx = findSplitIndex(msgs, 5)
	if idx != 0 {
		t.Errorf("findSplitIndex(keep=5) = %d, want 0", idx)
	}

	// Keep last 1
	idx = findSplitIndex(msgs, 1)
	if idx != 8 {
		t.Errorf("findSplitIndex(keep=1) = %d, want 8", idx)
	}
}
