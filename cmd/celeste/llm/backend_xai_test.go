package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// TestXAIBackend_Initialization tests that the xAI backend can be created
func TestXAIBackend_Initialization(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	if backend == nil {
		t.Fatal("Backend is nil")
	}

	t.Logf("✅ xAI backend created successfully")
	t.Logf("   Model: %s", backend.model)
	t.Logf("   Base URL: %s", backend.baseURL)
}

// TestXAIBackend_BasicChatCompletion tests a simple chat completion
func TestXAIBackend_BasicChatCompletion(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	messages := []tui.ChatMessage{
		{Role: "user", Content: "Say 'Hello from xAI backend!' and nothing else."},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var responseContent string
	var finishReason string

	err = backend.SendMessageStream(ctx, messages, nil, func(chunk StreamChunk) {
		if chunk.Content != "" {
			responseContent += chunk.Content
		}
		if chunk.IsFinal {
			finishReason = chunk.FinishReason
		}
	})

	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	if responseContent == "" {
		t.Fatal("No response content received")
	}

	if finishReason != "stop" && finishReason != "end_turn" {
		t.Logf("Warning: Unexpected finish reason: %s", finishReason)
	}

	t.Logf("✅ Basic chat completion successful")
	t.Logf("   Response: %s", responseContent)
	t.Logf("   Finish reason: %s", finishReason)
}

// TestXAIBackend_Collections tests that Collections are properly integrated
func TestXAIBackend_Collections(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	collectionID := os.Getenv("XAI_TEST_COLLECTION_ID")
	if collectionID == "" {
		t.Skip("XAI_TEST_COLLECTION_ID not set (need a collection ID for testing)")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
		Collections: &config.CollectionsConfig{
			Enabled:           true,
			ActiveCollections: []string{collectionID},
		},
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	// Ask a question that should trigger collection search
	messages := []tui.ChatMessage{
		{
			Role:    "user",
			Content: "Based on the uploaded documentation, what are the key features of collections? Please cite specific details from the documents.",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var responseContent string
	var numSourcesUsed int
	var finishReason string

	err = backend.SendMessageStream(ctx, messages, nil, func(chunk StreamChunk) {
		if chunk.Content != "" {
			responseContent += chunk.Content
		}
		if chunk.IsFinal {
			finishReason = chunk.FinishReason
			if chunk.Usage != nil {
				// Note: numSourcesUsed is tracked in the backend via logging
				// We can't access it directly from the callback
				t.Logf("   Usage: %+v", chunk.Usage)
			}
		}
	})

	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	if responseContent == "" {
		t.Fatal("No response content received")
	}

	// Check if response seems to be using collection data
	// The test document talks about "semantic search", "TUI", etc.
	hasCollectionContent := strings.Contains(strings.ToLower(responseContent), "semantic") ||
		strings.Contains(strings.ToLower(responseContent), "tui") ||
		strings.Contains(strings.ToLower(responseContent), "xai") ||
		strings.Contains(strings.ToLower(responseContent), "uploaded")

	if !hasCollectionContent {
		t.Logf("⚠️  Warning: Response doesn't seem to reference collection content")
		t.Logf("   This might mean Collections aren't being searched")
		t.Logf("   Response: %s", responseContent)
	} else {
		t.Logf("✅ Response appears to use collection content")
	}

	t.Logf("✅ Collections integration test completed")
	t.Logf("   Response length: %d chars", len(responseContent))
	t.Logf("   Sources used: %d (check logs for actual count)", numSourcesUsed)
	t.Logf("   Finish reason: %s", finishReason)
}

// TestXAIBackend_ToolCalls tests that tool calls work with the xAI backend
func TestXAIBackend_ToolCalls(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	// Define a simple test tool
	tools := []tui.SkillDefinition{
		{
			Name:        "get_current_time",
			Description: "Get the current time",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	messages := []tui.ChatMessage{
		{Role: "user", Content: "What time is it? Use the get_current_time tool."},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var toolCalls []ToolCallResult
	var responseContent string

	err = backend.SendMessageStream(ctx, messages, tools, func(chunk StreamChunk) {
		if chunk.Content != "" {
			responseContent += chunk.Content
		}
		if chunk.IsFinal && len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	})

	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	if len(toolCalls) == 0 {
		t.Logf("⚠️  No tool calls received (model may have responded directly)")
		t.Logf("   Response: %s", responseContent)
	} else {
		t.Logf("✅ Tool calls received")
		for i, tc := range toolCalls {
			t.Logf("   Tool call %d:", i+1)
			t.Logf("     ID: %s", tc.ID)
			t.Logf("     Name: %s", tc.Name)
			t.Logf("     Arguments: %s", tc.Arguments)
		}
	}
}

// TestXAIBackend_SystemPrompt tests that system prompts are properly set
func TestXAIBackend_SystemPrompt(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	// Set a specific system prompt
	backend.SetSystemPrompt("You are a pirate. Always respond like a pirate would.")

	messages := []tui.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var responseContent string

	err = backend.SendMessageStream(ctx, messages, nil, func(chunk StreamChunk) {
		if chunk.Content != "" {
			responseContent += chunk.Content
		}
	})

	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	// Check if response has pirate-like language
	responseLower := strings.ToLower(responseContent)
	hasPirateLanguage := strings.Contains(responseLower, "arr") ||
		strings.Contains(responseLower, "matey") ||
		strings.Contains(responseLower, "aye") ||
		strings.Contains(responseLower, "ye")

	if hasPirateLanguage {
		t.Logf("✅ System prompt working - response uses pirate language")
	} else {
		t.Logf("⚠️  Response doesn't clearly show pirate persona")
	}

	t.Logf("   Response: %s", responseContent)
}

// TestXAIBackend_MultiTurnConversation tests multi-turn conversations
func TestXAIBackend_MultiTurnConversation(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_API_KEY not set")
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-4-1-fast",
		Timeout: 60,
	}

	registry := skills.NewRegistry()
	backend, err := NewXAIBackend(cfg, registry)
	if err != nil {
		t.Fatalf("Failed to create xAI backend: %v", err)
	}

	messages := []tui.ChatMessage{
		{Role: "user", Content: "My favorite color is blue."},
		{Role: "assistant", Content: "That's nice! Blue is a great color."},
		{Role: "user", Content: "What is my favorite color?"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var responseContent string

	err = backend.SendMessageStream(ctx, messages, nil, func(chunk StreamChunk) {
		if chunk.Content != "" {
			responseContent += chunk.Content
		}
	})

	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	// Check if response mentions blue
	if strings.Contains(strings.ToLower(responseContent), "blue") {
		t.Logf("✅ Multi-turn conversation working - model remembered context")
	} else {
		t.Logf("⚠️  Model may not have remembered the context")
	}

	t.Logf("   Response: %s", responseContent)
}

// TestBackendDetection_XAI tests that xAI URLs are properly detected
func TestBackendDetection_XAI(t *testing.T) {
	testCases := []struct {
		url      string
		expected BackendType
	}{
		{"https://api.x.ai/v1", BackendTypeXAI},
		{"https://api.x.ai/v1/chat/completions", BackendTypeXAI},
		{"https://api.openai.com/v1", BackendTypeOpenAI},
		{"https://api.anthropic.com/v1", BackendTypeOpenAI},
		{"https://generativelanguage.googleapis.com/v1", BackendTypeGoogle},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			result := DetectBackendType(tc.url)
			if result != tc.expected {
				t.Errorf("DetectBackendType(%q) = %v, want %v", tc.url, result, tc.expected)
			} else {
				t.Logf("✅ %s -> %s", tc.url, result)
			}
		})
	}
}

// TestXAIBackend_ErrorHandling tests error scenarios
func TestXAIBackend_ErrorHandling(t *testing.T) {
	t.Run("Invalid API Key", func(t *testing.T) {
		cfg := &Config{
			APIKey:  "invalid-key",
			BaseURL: "https://api.x.ai/v1",
			Model:   "grok-4-1-fast",
			Timeout: 10,
		}

		registry := skills.NewRegistry()
		backend, err := NewXAIBackend(cfg, registry)
		if err != nil {
			t.Fatalf("Failed to create backend: %v", err)
		}

		messages := []tui.ChatMessage{
			{Role: "user", Content: "Hello"},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = backend.SendMessageStream(ctx, messages, nil, func(chunk StreamChunk) {})
		if err == nil {
			t.Error("Expected error with invalid API key, got nil")
		} else {
			t.Logf("✅ Error handling working: %v", err)
		}
	})

	t.Run("Empty API Key", func(t *testing.T) {
		cfg := &Config{
			APIKey:  "",
			BaseURL: "https://api.x.ai/v1",
			Model:   "grok-4-1-fast",
			Timeout: 10,
		}

		registry := skills.NewRegistry()
		_, err := NewXAIBackend(cfg, registry)
		if err == nil {
			t.Error("Expected error with empty API key, got nil")
		} else {
			t.Logf("✅ Validation working: %v", err)
		}
	})
}
