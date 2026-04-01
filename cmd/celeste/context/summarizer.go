package ctxmgr

import (
	"context"
	"fmt"
	"strings"
)

// Summarizer creates concise summaries of conversation message sequences.
// This is the core interface used by CompactionEngine.
type Summarizer interface {
	// Summarize takes a sequence of messages and returns a text summary
	// that preserves key context, decisions, and technical details.
	Summarize(ctx context.Context, messages []ChatMessage) (string, error)
}

// LLMClient is the minimal interface required to call an LLM for summarization.
// This avoids importing the llm package directly, preventing circular deps.
// The concrete implementation in main.go or wherever the LLM client is created
// should satisfy this interface.
type LLMClient interface {
	// SendSummarizationRequest sends a system+user prompt pair to the LLM
	// and returns the text response. No tool calling, no streaming.
	SendSummarizationRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMSummarizer implements Summarizer by calling an LLM to generate summaries.
// Migrated from llm/summarize.go.
type LLMSummarizer struct {
	client LLMClient
}

// NewLLMSummarizer creates a new LLM-backed summarizer.
func NewLLMSummarizer(client LLMClient) *LLMSummarizer {
	return &LLMSummarizer{client: client}
}

// summarizationSystemPrompt is the system prompt sent to the LLM for
// conversation summarization. It instructs the model to produce a concise
// summary preserving key context.
const summarizationSystemPrompt = `You are a conversation summarizer. Create a concise summary of the following conversation that preserves:
1. Key topics discussed
2. Important decisions or conclusions reached
3. Any action items or next steps
4. Essential context needed to continue the conversation
5. Technical details or specific information mentioned (file paths, function names, error messages, etc.)

The summary should be 150-250 words and written in a clear, factual style.
Do NOT include phrases like "the user asked" or "the assistant responded" -- just state the facts and conclusions directly.`

// Summarize creates a summary of the given messages by sending them to the LLM.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []ChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	// Build conversation text
	var b strings.Builder
	for _, msg := range messages {
		fmt.Fprintf(&b, "[%s]: %s\n\n", msg.Role, msg.Content)
	}

	userPrompt := fmt.Sprintf("Summarize the following conversation (%d messages):\n\n%s",
		len(messages), b.String())

	summary, err := s.client.SendSummarizationRequest(ctx, summarizationSystemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("summarization request failed: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", fmt.Errorf("LLM returned empty summary")
	}

	return summary, nil
}

// EstimateSummarySavings estimates tokens before and after summarization for
// the given message count. Useful for previewing compaction impact without
// actually calling the LLM.
func EstimateSummarySavings(messages []ChatMessage, count int) (before, after int) {
	if len(messages) == 0 || count <= 0 {
		return 0, 0
	}
	if count > len(messages) {
		count = len(messages)
	}

	for _, msg := range messages[:count] {
		before += EstimateTokens(msg.Content) + 4
	}

	// A good summary is typically 150-250 words, roughly 200-350 tokens.
	// We use 300 as the estimate plus formatting overhead.
	after = 300 + 50
	return before, after
}
