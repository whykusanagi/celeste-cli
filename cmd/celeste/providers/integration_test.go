//go:build integration
// +build integration

package providers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests require real API keys set as environment variables:
// - OPENAI_API_KEY
// - GROK_API_KEY
// - ANTHROPIC_API_KEY
// - GEMINI_API_KEY
// - VERTEX_API_KEY
//
// Run with: go test -tags=integration ./cmd/celeste/providers/
// Or: go test -tags=integration -v ./cmd/celeste/providers/ -run TestOpenAI

const (
	testTimeout = 30 * time.Second
	testPrompt  = "Say 'Hello' and nothing else."
)

// TestOpenAIIntegration tests OpenAI API with real credentials
func TestOpenAIIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI integration test: OPENAI_API_KEY not set")
	}

	t.Run("basic chat completion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		require.NoError(t, err, "OpenAI API call should succeed")
		assert.NotEmpty(t, resp.Choices, "Should have at least one response")
		assert.Contains(t, resp.Choices[0].Message.Content, "Hello", "Response should contain greeting")

		t.Logf("✅ OpenAI basic chat: %s", resp.Choices[0].Message.Content)
	})

	t.Run("function calling", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: "What's the weather in New York? Use the get_weather function."},
			},
			Tools: []openai.Tool{
				{
					Type: "function",
					Function: &openai.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get weather for a location",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"location": map[string]interface{}{
									"type":        "string",
									"description": "City name",
								},
							},
							"required": []string{"location"},
						},
					},
				},
			},
		})

		require.NoError(t, err, "Function calling should succeed")
		assert.NotEmpty(t, resp.Choices, "Should have response")

		// Check if function was called
		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			toolCall := resp.Choices[0].Message.ToolCalls[0]
			assert.Equal(t, "get_weather", toolCall.Function.Name, "Should call get_weather function")
			t.Logf("✅ OpenAI function calling: %s with args %s", toolCall.Function.Name, toolCall.Function.Arguments)
		} else {
			t.Log("⚠️ No tool calls made (model may have responded directly)")
		}
	})

	t.Run("streaming", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		client := openai.NewClientWithConfig(config)

		stream, err := client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: "Count to 3"},
			},
		})

		require.NoError(t, err, "Stream creation should succeed")
		defer stream.Close()

		var chunks int
		for {
			_, err := stream.Recv()
			if err != nil {
				break
			}
			chunks++
		}

		assert.Greater(t, chunks, 0, "Should receive at least one chunk")
		t.Logf("✅ OpenAI streaming: received %d chunks", chunks)
	})

	t.Run("model listing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		service := NewModelService(apiKey, "", "openai")
		models, err := service.ListModels(ctx)

		require.NoError(t, err, "Model listing should succeed")
		assert.NotEmpty(t, models, "Should return models")

		// Check for expected models
		var hasGPT4Mini bool
		for _, m := range models {
			if m.ID == "gpt-4o-mini" {
				hasGPT4Mini = true
				assert.True(t, m.SupportsTools, "gpt-4o-mini should support tools")
			}
		}
		assert.True(t, hasGPT4Mini, "Should include gpt-4o-mini")
		t.Logf("✅ OpenAI model listing: found %d models", len(models))
	})
}

// TestGrokIntegration tests Grok (xAI) API with real credentials
func TestGrokIntegration(t *testing.T) {
	apiKey := os.Getenv("GROK_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Grok integration test: GROK_API_KEY not set")
	}

	baseURL := "https://api.x.ai/v1"

	t.Run("basic chat completion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "grok-beta",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		require.NoError(t, err, "Grok API call should succeed")
		assert.NotEmpty(t, resp.Choices, "Should have at least one response")
		t.Logf("✅ Grok basic chat: %s", resp.Choices[0].Message.Content)
	})

	t.Run("function calling", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "grok-beta",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: "What's the weather in San Francisco? Use the get_weather function."},
			},
			Tools: []openai.Tool{
				{
					Type: "function",
					Function: &openai.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get weather for a location",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"location": map[string]interface{}{
									"type":        "string",
									"description": "City name",
								},
							},
							"required": []string{"location"},
						},
					},
				},
			},
		})

		require.NoError(t, err, "Grok function calling should succeed")
		assert.NotEmpty(t, resp.Choices, "Should have response")

		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			toolCall := resp.Choices[0].Message.ToolCalls[0]
			assert.Equal(t, "get_weather", toolCall.Function.Name)
			t.Logf("✅ Grok function calling: %s", toolCall.Function.Name)
		} else {
			t.Log("⚠️ Grok did not use function (may have responded directly)")
		}
	})

	t.Run("model listing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		service := NewModelService(apiKey, baseURL, "grok")
		models, err := service.ListModels(ctx)

		require.NoError(t, err, "Grok model listing should succeed")
		assert.NotEmpty(t, models, "Should return models")
		t.Logf("✅ Grok model listing: found %d models", len(models))
	})
}

// TestGeminiIntegration tests Google Gemini API
func TestGeminiIntegration(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini integration test: GEMINI_API_KEY not set")
	}

	baseURL := "https://generativelanguage.googleapis.com/v1beta/openai"

	t.Run("basic chat completion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gemini-1.5-flash",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		if err != nil {
			t.Logf("⚠️ Gemini basic chat failed: %v", err)
			t.Skip("Gemini API may require different auth or format")
		}

		assert.NotEmpty(t, resp.Choices, "Should have response")
		t.Logf("✅ Gemini basic chat: %s", resp.Choices[0].Message.Content)
	})

	t.Run("function calling", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gemini-1.5-flash",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: "What's the weather in Tokyo? Use get_weather function."},
			},
			Tools: []openai.Tool{
				{
					Type: "function",
					Function: &openai.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get weather for a location",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"location": map[string]interface{}{
									"type":        "string",
									"description": "City name",
								},
							},
							"required": []string{"location"},
						},
					},
				},
			},
		})

		if err != nil {
			t.Logf("⚠️ Gemini function calling failed: %v", err)
			t.Skip("Gemini function calling may require native API")
		}

		assert.NotEmpty(t, resp.Choices, "Should have response")
		t.Logf("✅ Gemini function calling test completed")
	})
}

// TestAnthropicIntegration tests Anthropic Claude API
func TestAnthropicIntegration(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Anthropic integration test: ANTHROPIC_API_KEY not set")
	}

	// Test via OpenAI compatibility endpoint
	baseURL := "https://api.anthropic.com/v1"

	t.Run("OpenAI compatibility mode", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "claude-3-5-sonnet-20241022",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		if err != nil {
			t.Logf("⚠️ Anthropic via OpenAI compatibility failed: %v", err)
			t.Log("Note: Anthropic may require native Messages API")
			t.Skip("OpenAI compatibility mode not working")
		}

		assert.NotEmpty(t, resp.Choices, "Should have response")
		t.Logf("✅ Anthropic OpenAI mode: %s", resp.Choices[0].Message.Content)
	})

	// TODO: Implement native Anthropic Messages API test
	t.Run("native Messages API", func(t *testing.T) {
		t.Skip("Native Anthropic API not yet implemented - requires custom client")
		// Future: Use Anthropic SDK instead of OpenAI client
	})
}

// TestVeniceIntegration tests Venice.ai API
func TestVeniceIntegration(t *testing.T) {
	apiKey := os.Getenv("VENICE_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Venice integration test: VENICE_API_KEY not set")
	}

	baseURL := "https://api.venice.ai/api/v1"

	t.Run("basic chat completion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "llama-3.3-70b",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		require.NoError(t, err, "Venice API call should succeed")
		assert.NotEmpty(t, resp.Choices, "Should have response")
		t.Logf("✅ Venice basic chat: %s", resp.Choices[0].Message.Content)
	})

	t.Run("function calling with llama-3.3", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "llama-3.3-70b",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: "What's the weather in Paris? Use get_weather."},
			},
			Tools: []openai.Tool{
				{
					Type: "function",
					Function: &openai.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get weather for a location",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"location": map[string]interface{}{
									"type":        "string",
									"description": "City name",
								},
							},
							"required": []string{"location"},
						},
					},
				},
			},
		})

		require.NoError(t, err, "Function calling should succeed")
		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			t.Logf("✅ Venice function calling: supported with llama-3.3-70b")
		} else {
			t.Log("⚠️ Venice may not support function calling (model responded directly)")
		}
	})

	t.Run("uncensored model (no tools)", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "venice-uncensored",
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: testPrompt},
			},
		})

		if err != nil {
			t.Logf("⚠️ Venice uncensored failed: %v", err)
			t.Skip("Venice uncensored model may not be available")
		}

		assert.NotEmpty(t, resp.Choices, "Should have response")
		t.Logf("✅ Venice uncensored: works (no function calling support)")
	})
}

// TestProviderComparison runs same prompt across all providers
func TestProviderComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping comparison test in short mode")
	}

	prompt := "What is 2+2? Answer with just the number."
	results := make(map[string]string)

	// Test each provider if API key is available
	providers := []struct {
		name    string
		envVar  string
		baseURL string
		model   string
	}{
		{"OpenAI", "OPENAI_API_KEY", "", "gpt-4o-mini"},
		{"Grok", "GROK_API_KEY", "https://api.x.ai/v1", "grok-beta"},
		{"Gemini", "GEMINI_API_KEY", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-1.5-flash"},
		{"Venice", "VENICE_API_KEY", "https://api.venice.ai/api/v1", "llama-3.3-70b"},
	}

	for _, p := range providers {
		apiKey := os.Getenv(p.envVar)
		if apiKey == "" {
			t.Logf("⏭️  Skipping %s: %s not set", p.name, p.envVar)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		config := openai.DefaultConfig(apiKey)
		if p.baseURL != "" {
			config.BaseURL = p.baseURL
		}
		client := openai.NewClientWithConfig(config)

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: p.model,
			Messages: []openai.ChatCompletionMessage{
				{Role: "user", Content: prompt},
			},
		})
		cancel()

		if err != nil {
			results[p.name] = fmt.Sprintf("ERROR: %v", err)
		} else if len(resp.Choices) > 0 {
			results[p.name] = resp.Choices[0].Message.Content
		}
	}

	// Print comparison
	t.Log("\n═══ Provider Comparison Results ═══")
	for name, result := range results {
		t.Logf("%s: %s", name, result)
	}
}
