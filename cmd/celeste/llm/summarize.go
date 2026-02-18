package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Summarizer handles automatic conversation summarization for context management.
type Summarizer struct {
	client *Client
}

// NewSummarizer creates a new conversation summarizer.
func NewSummarizer(client *Client) *Summarizer {
	return &Summarizer{
		client: client,
	}
}

// SummarizeMessages creates a concise summary of conversation messages.
// Returns a summary message that preserves key context while reducing token count.
func (s *Summarizer) SummarizeMessages(messages []config.SessionMessage, count int) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	if count <= 0 || count > len(messages) {
		count = len(messages)
	}

	// Build the conversation text to summarize
	var conversationText strings.Builder
	messagesToSummarize := messages[:count]

	for _, msg := range messagesToSummarize {
		conversationText.WriteString(fmt.Sprintf("%s: %s\n\n", msg.Role, msg.Content))
	}

	// Create summarization prompt
	systemPrompt := `You are a conversation summarizer. Create a concise summary of the following conversation that preserves:
1. Key topics discussed
2. Important decisions or conclusions reached
3. Any action items or next steps
4. Essential context needed to continue the conversation
5. Technical details or specific information mentioned

The summary should be 150-250 words and written in a clear, factual style.`

	userPrompt := fmt.Sprintf("Please summarize the following conversation:\n\n%s", conversationText.String())

	// Call LLM for summarization
	ctx := context.Background()

	// Build messages for summarization request
	summaryMessages := []tui.ChatMessage{
		{Role: "system", Content: systemPrompt, Timestamp: time.Now()},
		{Role: "user", Content: userPrompt, Timestamp: time.Now()},
	}

	// Send to LLM (synchronous, no tools needed)
	result, err := s.client.SendMessageSync(ctx, summaryMessages, nil)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("summarization error: %w", result.Error)
	}

	if result.Content == "" {
		return "", fmt.Errorf("empty summary returned")
	}

	return result.Content, nil
}

// CompactSession performs context compaction by summarizing old messages.
// targetTokens specifies the desired token count after compaction (typically 70% of max).
// Returns the number of messages before and after compaction.
func (s *Summarizer) CompactSession(session *config.Session, targetTokens int) (int, int, error) {
	if session == nil {
		return 0, 0, fmt.Errorf("session is nil")
	}

	messages := session.GetMessages()
	if len(messages) == 0 {
		return 0, 0, fmt.Errorf("no messages to compact")
	}

	messagesBefore := len(messages)
	currentTokens := session.TokenCount

	if currentTokens <= targetTokens {
		// Already under target, no compaction needed
		return messagesBefore, messagesBefore, nil
	}

	// Calculate how many messages to summarize
	// Strategy: Keep recent messages, summarize older ones
	// Aim to reduce by ~30-40% of current token count
	tokensToSave := currentTokens - targetTokens
	avgTokensPerMessage := currentTokens / len(messages)
	if avgTokensPerMessage == 0 {
		avgTokensPerMessage = 500 // Default estimate
	}

	// Estimate messages to summarize (will be replaced with summary)
	messagesToSummarize := tokensToSave / avgTokensPerMessage
	if messagesToSummarize < 2 {
		messagesToSummarize = 2 // Minimum for meaningful summary
	}
	if messagesToSummarize > len(messages)-2 {
		messagesToSummarize = len(messages) - 2 // Keep at least 2 recent messages
	}

	// Don't summarize the very first system message if it exists
	startIndex := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		startIndex = 1
		messagesToSummarize-- // Don't count system message
	}

	// Ensure we have messages to summarize
	if messagesToSummarize <= 0 {
		return messagesBefore, messagesBefore, fmt.Errorf("insufficient messages for compaction")
	}

	// Get the messages to summarize
	endIndex := startIndex + messagesToSummarize
	if endIndex > len(messages) {
		endIndex = len(messages)
	}

	// Create summary
	summary, err := s.SummarizeMessages(messages[startIndex:endIndex], messagesToSummarize)
	if err != nil {
		return messagesBefore, messagesBefore, fmt.Errorf("failed to create summary: %w", err)
	}

	// Build new message slice with summary
	var newMessages []config.SessionMessage

	// Keep system message if it exists
	if startIndex > 0 {
		newMessages = append(newMessages, messages[0])
	}

	// Add summary as a system message
	newMessages = append(newMessages, config.SessionMessage{
		Role:      "system",
		Content:   fmt.Sprintf("📋 Conversation Summary (messages 1-%d):\n\n%s", messagesToSummarize, summary),
		Timestamp: messages[startIndex].Timestamp, // Use timestamp of first summarized message
	})

	// Keep remaining recent messages
	if endIndex < len(messages) {
		newMessages = append(newMessages, messages[endIndex:]...)
	}

	// Update session with compacted messages
	session.Messages = newMessages

	// Update token count estimate
	session.TokenCount = config.EstimateSessionTokens(session)

	// Update usage metrics if available
	if session.UsageMetrics != nil {
		session.UsageMetrics.CompactionCount++
	}

	messagesAfter := len(newMessages)
	return messagesBefore, messagesAfter, nil
}

// EstimateSummarySavings estimates how many tokens would be saved by summarization.
// Returns estimated tokens before and after summarization.
func EstimateSummarySavings(messages []config.SessionMessage, count int) (int, int) {
	if len(messages) == 0 || count <= 0 {
		return 0, 0
	}

	if count > len(messages) {
		count = len(messages)
	}

	// Estimate current tokens for these messages
	var totalContent string
	for _, msg := range messages[:count] {
		totalContent += msg.Content
	}
	beforeTokens := config.EstimateTokens(totalContent)

	// Estimate summary tokens (typically 200-300 tokens for a good summary)
	// Add overhead for summary message formatting
	summaryTokens := 250 + 50 // 250 for summary + 50 for formatting

	return beforeTokens, summaryTokens
}

// ValidateCompactionSavings checks if compaction would achieve meaningful token savings.
// Returns true if savings would be at least 20% of current tokens.
func ValidateCompactionSavings(currentTokens, targetTokens int) bool {
	if currentTokens <= targetTokens {
		return false // Already under target
	}

	savings := currentTokens - targetTokens
	savingsPercent := float64(savings) / float64(currentTokens)

	return savingsPercent >= 0.20 // Require at least 20% savings
}

// FormatCompactionResult creates a user-friendly message about compaction results.
func FormatCompactionResult(messagesBefore, messagesAfter, tokensBefore, tokensAfter int) string {
	tokensSaved := tokensBefore - tokensAfter
	savingsPercent := float64(tokensSaved) / float64(tokensBefore) * 100

	return fmt.Sprintf(
		"✓ Auto-compacted: %d msgs → %d msgs (saved %s tokens, %.1f%% reduction)",
		messagesBefore,
		messagesAfter,
		config.FormatTokenCount(tokensSaved),
		savingsPercent,
	)
}

// ShouldTriggerCompaction determines if automatic compaction should trigger.
// Checks if session has reached 80% of context window capacity.
func ShouldTriggerCompaction(session *config.Session, model string) bool {
	if session == nil {
		return false
	}

	maxTokens := config.GetModelLimit(model)
	if maxTokens == 0 {
		return false
	}

	currentTokens := session.TokenCount
	usagePercent := float64(currentTokens) / float64(maxTokens)

	return usagePercent >= 0.80 // Trigger at 80%
}

// CalculateTargetTokens calculates the target token count for compaction.
// Typically aims for 70% of max context window.
func CalculateTargetTokens(maxTokens int) int {
	return int(float64(maxTokens) * 0.70)
}

// MarshalSummaryMetadata creates JSON metadata about the summarization.
// Useful for logging and debugging.
func MarshalSummaryMetadata(messagesBefore, messagesAfter, tokensBefore, tokensAfter int) (string, error) {
	metadata := map[string]interface{}{
		"messages_before": messagesBefore,
		"messages_after":  messagesAfter,
		"tokens_before":   tokensBefore,
		"tokens_after":    tokensAfter,
		"messages_saved":  messagesBefore - messagesAfter,
		"tokens_saved":    tokensBefore - tokensAfter,
		"savings_percent": float64(tokensBefore-tokensAfter) / float64(tokensBefore) * 100,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}
