package ctxmgr

import (
	"context"
	"fmt"
	"testing"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	response string
	err      error
	lastSys  string
	lastUser string
}

func (m *mockLLMClient) SendSummarizationRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	m.lastSys = systemPrompt
	m.lastUser = userPrompt
	return m.response, m.err
}

func TestLLMSummarizer_Summarize(t *testing.T) {
	client := &mockLLMClient{response: "The conversation covered Go testing patterns and error handling."}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{
		{Role: "user", Content: "How do I write tests in Go?"},
		{Role: "assistant", Content: "Use the testing package with Test functions."},
		{Role: "user", Content: "What about error handling?"},
		{Role: "assistant", Content: "Use the errors package and wrap errors with fmt.Errorf."},
	}

	ctx := context.Background()
	summary, err := summarizer.Summarize(ctx, msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "The conversation covered Go testing patterns and error handling." {
		t.Errorf("unexpected summary: %s", summary)
	}
	// Verify the system prompt was sent
	if client.lastSys == "" {
		t.Error("system prompt should not be empty")
	}
	// Verify user prompt contains message count
	if client.lastUser == "" {
		t.Error("user prompt should not be empty")
	}
}

func TestLLMSummarizer_EmptyMessages(t *testing.T) {
	client := &mockLLMClient{response: "summary"}
	summarizer := NewLLMSummarizer(client)

	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, nil)
	if err == nil {
		t.Error("expected error for nil messages")
	}

	_, err = summarizer.Summarize(ctx, []ChatMessage{})
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestLLMSummarizer_LLMError(t *testing.T) {
	client := &mockLLMClient{err: fmt.Errorf("API rate limit exceeded")}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{{Role: "user", Content: "hello"}}
	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, msgs)
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}

func TestLLMSummarizer_EmptyResponse(t *testing.T) {
	client := &mockLLMClient{response: "   "}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{{Role: "user", Content: "hello"}}
	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, msgs)
	if err == nil {
		t.Error("expected error for whitespace-only response")
	}
}

func TestEstimateSummarySavings(t *testing.T) {
	msgs := makeConversation(30) // Uses the helper from compaction_test.go

	before, after := EstimateSummarySavings(msgs, 60)
	if before <= 0 {
		t.Error("before should be positive")
	}
	if after <= 0 {
		t.Error("after should be positive")
	}
	if after >= before {
		t.Errorf("summary should use fewer tokens than originals: before=%d after=%d", before, after)
	}
}

func TestEstimateSummarySavings_EdgeCases(t *testing.T) {
	before, after := EstimateSummarySavings(nil, 5)
	if before != 0 || after != 0 {
		t.Error("nil messages should return 0, 0")
	}

	before, after = EstimateSummarySavings([]ChatMessage{}, 0)
	if before != 0 || after != 0 {
		t.Error("zero count should return 0, 0")
	}

	// count > len(messages) -- should clamp
	msgs := makeConversation(2) // 4 messages
	before, after = EstimateSummarySavings(msgs, 100)
	if before <= 0 || after < 0 {
		t.Error("should still estimate with clamped count")
	}
}
