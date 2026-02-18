// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	genai "google.golang.org/genai"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// GoogleBackend implements LLMBackend using Google's native GenAI SDK.
// This backend supports Gemini AI Studio and Vertex AI with automatic authentication.
type GoogleBackend struct {
	client       *genai.Client
	config       *Config
	systemPrompt string
}

// NewGoogleBackend creates a new Google GenAI backend with automatic authentication.
// Authentication methods (in order of priority):
// 1. Simple API key (for Gemini AI Studio)
// 2. GoogleCredentialsFile in config (service account JSON)
// 3. GOOGLE_APPLICATION_CREDENTIALS environment variable
// 4. Application Default Credentials (gcloud auth application-default login)
func NewGoogleBackend(config *Config) (*GoogleBackend, error) {
	ctx := context.Background()

	// Create client configuration with API version
	clientConfig := &genai.ClientConfig{
		HTTPOptions: genai.HTTPOptions{
			APIVersion: "v1",
		},
	}

	// Method 1: Simple API key (most common for Gemini AI Studio)
	if config.APIKey != "" && !strings.HasPrefix(config.APIKey, "ya29.") {
		// Note: OAuth2 tokens start with "ya29." - those should use ADC instead
		clientConfig.APIKey = config.APIKey
	} else if config.GoogleCredentialsFile != "" {
		// Method 2: Service account JSON file
		if _, err := os.Stat(config.GoogleCredentialsFile); os.IsNotExist(err) {
			return nil, fmt.Errorf("Google credentials file not found: %s", config.GoogleCredentialsFile)
		}
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", config.GoogleCredentialsFile)
	}
	// Method 3 & 4: Environment variable or ADC (handled automatically by SDK)

	// Override base URL if needed (for Vertex AI)
	if config.BaseURL != "" && !strings.Contains(config.BaseURL, "generativelanguage.googleapis.com") {
		clientConfig.HTTPOptions.BaseURL = config.BaseURL
	}

	// Create the client - SDK will auto-detect credentials
	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Google AI client: %w\n\n"+
			"Authentication options:\n"+
			"1. Get API key from: https://aistudio.google.com/ (for Gemini AI Studio)\n"+
			"2. Run: gcloud auth application-default login\n"+
			"3. Set: GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json\n"+
			"4. Use: celeste config --set-google-credentials /path/to/service-account.json\n"+
			"See: https://cloud.google.com/docs/authentication", err)
	}

	return &GoogleBackend{
		client: client,
		config: config,
	}, nil
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (b *GoogleBackend) SetSystemPrompt(prompt string) {
	b.systemPrompt = prompt
}

// SendMessageSync sends a message synchronously and returns the complete result.
func (b *GoogleBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	// Convert messages to Google GenAI format
	contents := b.convertMessagesToGenAI(messages)

	// Convert tools to Google function declarations
	var functionDeclarations []*genai.FunctionDeclaration
	if len(tools) > 0 {
		functionDeclarations = b.convertToolsToGenAI(tools)
	}

	// Create generation config
	genConfig := &genai.GenerateContentConfig{}

	// Add system instruction if present
	if b.systemPrompt != "" && !b.config.SkipPersonaPrompt {
		// System instruction doesn't need a role - it's handled differently
		genConfig.SystemInstruction = genai.NewContentFromText(b.systemPrompt, "user")
	}

	if len(functionDeclarations) > 0 {
		genConfig.Tools = []*genai.Tool{
			{FunctionDeclarations: functionDeclarations},
		}
	}

	// Generate content
	modelName := b.config.Model
	resp, err := b.client.Models.GenerateContent(ctx, modelName, contents, genConfig)
	if err != nil {
		return nil, fmt.Errorf("Google AI request failed: %w", err)
	}

	// Parse response
	result := &ChatCompletionResult{}

	if len(resp.Candidates) > 0 {
		candidate := resp.Candidates[0]

		// Extract text content
		if candidate.Content != nil {
			result.Content = extractText(candidate.Content)
		}

		// Extract tool calls (function calls)
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if part.FunctionCall != nil {
					toolCall := b.convertFunctionCallToResult(part.FunctionCall)
					result.ToolCalls = append(result.ToolCalls, toolCall)
				}
			}
		}

		// Extract finish reason
		if candidate.FinishReason != "" {
			result.FinishReason = string(candidate.FinishReason)
		}
	}

	return result, nil
}

// SendMessageStream sends a message with streaming callback.
func (b *GoogleBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	// Convert messages to Google GenAI format
	contents := b.convertMessagesToGenAI(messages)

	// Convert tools to Google function declarations
	var functionDeclarations []*genai.FunctionDeclaration
	if len(tools) > 0 {
		functionDeclarations = b.convertToolsToGenAI(tools)
	}

	// Create generation config
	genConfig := &genai.GenerateContentConfig{}

	// Add system instruction if present
	if b.systemPrompt != "" && !b.config.SkipPersonaPrompt {
		// System instruction doesn't need a role - it's handled differently
		genConfig.SystemInstruction = genai.NewContentFromText(b.systemPrompt, "user")
	}

	if len(functionDeclarations) > 0 {
		genConfig.Tools = []*genai.Tool{
			{FunctionDeclarations: functionDeclarations},
		}
	}

	// Stream the response
	modelName := b.config.Model
	streamIter := b.client.Models.GenerateContentStream(ctx, modelName, contents, genConfig)

	var fullContent strings.Builder
	var toolCalls []ToolCallResult
	isFirst := true
	var lastFinishReason string

	// Iterate over streaming chunks
	for chunk, err := range streamIter {
		if err != nil {
			return fmt.Errorf("Google AI stream error: %w", err)
		}

		// Process each candidate in the chunk
		for _, candidate := range chunk.Candidates {
			streamChunk := StreamChunk{
				IsFirst: isFirst,
			}

			// Extract text content
			if candidate.Content != nil {
				text := extractText(candidate.Content)
				if text != "" {
					fullContent.WriteString(text)
					streamChunk.Content = text
				}

				// Extract function calls (tool calls)
				for _, part := range candidate.Content.Parts {
					if part.FunctionCall != nil {
						toolCall := b.convertFunctionCallToResult(part.FunctionCall)
						toolCalls = append(toolCalls, toolCall)
					}
				}
			}

			// Check finish reason
			if candidate.FinishReason != "" {
				lastFinishReason = string(candidate.FinishReason)
			}

			// Call callback with chunk (if there's content or it's the first chunk)
			if streamChunk.Content != "" || isFirst {
				callback(streamChunk)
				isFirst = false
			}
		}
	}

	// Send final chunk with complete tool calls and finish reason
	callback(StreamChunk{
		IsFinal:      true,
		FinishReason: lastFinishReason,
		ToolCalls:    toolCalls,
		Usage:        nil, // Google GenAI SDK doesn't provide token usage in streaming yet
	})

	return nil
}

// Close cleans up resources.
func (b *GoogleBackend) Close() error {
	// Google GenAI SDK client doesn't require explicit cleanup
	return nil
}

// convertMessagesToGenAI converts Celeste messages to Google GenAI format.
func (b *GoogleBackend) convertMessagesToGenAI(messages []tui.ChatMessage) []*genai.Content {
	var contents []*genai.Content

	// Skip system prompt - it's handled via SystemInstruction in config
	for _, msg := range messages {
		if msg.Role == "system" {
			continue // System messages are handled separately
		}

		// Convert role: "assistant" -> "model" for Google
		role := msg.Role
		if role == "assistant" {
			role = genai.RoleModel
		} else if role == "user" {
			role = genai.RoleUser
		}

		// Handle tool responses (function responses)
		if msg.Role == "tool" {
			// Tool responses need special handling in Google format
			// They should be added as function response parts
			part := genai.NewPartFromFunctionResponse(msg.ToolCallID, map[string]any{
				"result": msg.Content,
			})

			// Function responses use "user" role in Google GenAI
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{part}, genai.RoleUser))
			continue
		}

		// Handle assistant messages with tool calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			parts := []*genai.Part{}

			// Add text content if present
			if msg.Content != "" {
				parts = append(parts, genai.NewPartFromText(msg.Content))
			}

			// Add function calls
			for _, tc := range msg.ToolCalls {
				// Parse arguments JSON
				var args map[string]any
				if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
					// If parsing fails, use empty args
					args = make(map[string]any)
				}

				parts = append(parts, genai.NewPartFromFunctionCall(tc.Name, args))
			}

			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))
			continue
		}

		// Regular text messages
		if msg.Content != "" {
			contents = append(contents, genai.NewContentFromText(msg.Content, genai.Role(role)))
		}
	}

	return contents
}

// convertToolsToGenAI converts OpenAI-style tools to Google function declarations.
func (b *GoogleBackend) convertToolsToGenAI(tools []tui.SkillDefinition) []*genai.FunctionDeclaration {
	var declarations []*genai.FunctionDeclaration

	for _, tool := range tools {
		// Convert OpenAI JSON schema to Google schema
		schema := b.convertSchemaToGenAI(tool.Parameters)

		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}

	return declarations
}

// convertSchemaToGenAI converts OpenAI JSON schema format to Google GenAI schema format.
func (b *GoogleBackend) convertSchemaToGenAI(params map[string]interface{}) *genai.Schema {
	schema := &genai.Schema{
		Type: genai.TypeObject,
	}

	// Extract properties
	if props, ok := params["properties"].(map[string]interface{}); ok {
		properties := make(map[string]*genai.Schema)

		for propName, propValue := range props {
			if propMap, ok := propValue.(map[string]interface{}); ok {
				propSchema := &genai.Schema{}

				// Convert type
				if typeStr, ok := propMap["type"].(string); ok {
					propSchema.Type = convertTypeToGenAI(typeStr)
				}

				// Convert description
				if desc, ok := propMap["description"].(string); ok {
					propSchema.Description = desc
				}

				// Convert enum values
				if enum, ok := propMap["enum"].([]interface{}); ok {
					enumStrs := make([]string, len(enum))
					for i, v := range enum {
						if str, ok := v.(string); ok {
							enumStrs[i] = str
						}
					}
					propSchema.Enum = enumStrs
				}

				// Handle nested objects (arrays)
				if propSchema.Type == genai.TypeArray {
					if items, ok := propMap["items"].(map[string]interface{}); ok {
						propSchema.Items = b.convertSchemaToGenAI(map[string]interface{}{
							"properties": items,
						})
					}
				}

				properties[propName] = propSchema
			}
		}

		schema.Properties = properties
	}

	// Extract required fields
	if required, ok := params["required"].([]interface{}); ok {
		requiredStrs := make([]string, len(required))
		for i, v := range required {
			if str, ok := v.(string); ok {
				requiredStrs[i] = str
			}
		}
		schema.Required = requiredStrs
	}

	return schema
}

// convertTypeToGenAI converts OpenAI JSON schema types to Google GenAI types.
func convertTypeToGenAI(typeStr string) genai.Type {
	switch typeStr {
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	case "object":
		return genai.TypeObject
	default:
		return genai.TypeString // Default fallback
	}
}

// convertFunctionCallToResult converts Google FunctionCall to our ToolCallResult format.
func (b *GoogleBackend) convertFunctionCallToResult(fc *genai.FunctionCall) ToolCallResult {
	// Generate a tool call ID (Google doesn't provide one)
	toolCallID := fmt.Sprintf("call_%s", fc.Name)

	// Convert arguments to JSON string
	argsJSON := "{}"
	if fc.Args != nil {
		// Marshal the args map to JSON
		if jsonBytes, err := json.Marshal(fc.Args); err == nil {
			argsJSON = string(jsonBytes)
		}
	}

	return ToolCallResult{
		ID:        toolCallID,
		Name:      fc.Name,
		Arguments: argsJSON,
	}
}

// extractText extracts text content from a Google GenAI Content object.
func extractText(content *genai.Content) string {
	var text strings.Builder

	for _, part := range content.Parts {
		if part.Text != "" {
			text.WriteString(part.Text)
		}
	}

	return text.String()
}
