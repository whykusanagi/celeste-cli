// Package llm provides the LLM client for Celeste CLI.
package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	genai "google.golang.org/genai"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/imagefit"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// GoogleBackend implements LLMBackend using Google's native GenAI SDK.
// This backend supports Gemini AI Studio and Vertex AI with automatic authentication.
type GoogleBackend struct {
	client         *genai.Client
	config         *Config
	mu             sync.Mutex // guards systemPrompt and thinkingConfig
	systemPrompt   string
	thinkingConfig ThinkingConfig
}

// googleAPIVersion picks the Google API version for a base URL.
//
// AI Studio (generativelanguage.googleapis.com) must use v1beta. Google retired
// v1 for current models: verified 2026-08-08 that gemini-3.6-flash and
// gemini-2.5-flash both return 404 on v1 while succeeding on v1beta, and
// gemini-2.0-flash answers "This model is no longer available". Note /v1/models
// still LISTS those models with generateContent in supportedGenerationMethods,
// so the listing endpoint cannot be trusted to tell you what is callable.
//
// Vertex (aiplatform.googleapis.com) keeps v1, which is its own convention and
// a separate service — do not fold the two together.
func googleAPIVersion(baseURL string) string {
	if baseURL == "" || providers.IsGeminiURL(baseURL) {
		return "v1beta"
	}
	return "v1"
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
			APIVersion: googleAPIVersion(config.BaseURL),
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
	if config.BaseURL != "" && !providers.IsGeminiURL(config.BaseURL) {
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

	// Report response bytes to the stall watch, as every other backend's
	// HTTP client does (newHTTPClient). The SDK built its own client (with
	// the ADC/Vertex auth transport when that applies), so wrap that one's
	// transport rather than passing ours in: a custom ClientConfig.HTTPClient
	// makes the SDK skip credential detection.
	if hc := client.ClientConfig().HTTPClient; hc != nil {
		hc.Transport = stallTransport{base: hc.Transport}
	}

	return &GoogleBackend{
		client: client,
		config: config,
	}, nil
}

// SetSystemPrompt sets the system prompt (Celeste persona).
func (b *GoogleBackend) SetSystemPrompt(prompt string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.systemPrompt = prompt
}

// SetThinkingConfig configures extended thinking for Gemini models.
// Gemini supports thinkingBudget via GenerateContentConfig.ThinkingConfig.
func (b *GoogleBackend) SetThinkingConfig(config ThinkingConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.thinkingConfig = config
}

// prompt and thinking read the settings the setters change; a request may
// be building while the client sets them.
func (b *GoogleBackend) prompt() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.systemPrompt
}

func (b *GoogleBackend) thinking() ThinkingConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.thinkingConfig
}

// request builds the contents and generation config every send uses.
func (b *GoogleBackend) request(messages []tui.ChatMessage, tools []tui.SkillDefinition) ([]*genai.Content, *genai.GenerateContentConfig) {
	contents := b.convertMessagesToGenAI(messages)
	genConfig := &genai.GenerateContentConfig{}
	if prompt := b.prompt(); prompt != "" {
		// System instruction doesn't need a role - it's handled differently
		genConfig.SystemInstruction = genai.NewContentFromText(prompt, "user")
	}
	if len(tools) > 0 {
		if decls := b.convertToolsToGenAI(tools); len(decls) > 0 {
			genConfig.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
		}
	}
	b.applyThinkingConfig(genConfig)
	return contents, genConfig
}

// SendMessageSync sends a message and returns the complete result. It
// streams internally (#349): a whole-reply request sends nothing until the
// reply is complete, so a long one (a compaction summary) looked idle and
// failed with ErrStalled after the stall timeout. Streamed, every chunk's
// bytes reach the stall watch, and each chunk also counts as activity. It
// accumulates SendMessageStreamEvents, the backend's one reading of the
// stream (audit C3).
func (b *GoogleBackend) SendMessageSync(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error) {
	result := &ChatCompletionResult{}
	var content strings.Builder
	acc := NewToolUseAccumulator()
	err := b.streamEvents(ctx, messages, tools, func(ev StreamEvent) {
		acc.HandleEvent(ev)
		switch ev.Type {
		case EventContentDelta:
			content.WriteString(ev.ContentDelta)
		case EventMessageDone:
			result.FinishReason = ev.FinishReason
		}
	})
	if err != nil {
		return nil, fmt.Errorf("Google AI request failed: %w", err)
	}
	result.Content = content.String()
	result.ToolCalls = acc.CompletedCalls()
	return result, nil
}

// SendMessageStream sends a message with streaming callback: the text of
// SendMessageStreamEvents as chunks, then one final chunk with the complete
// tool calls and the finish reason.
func (b *GoogleBackend) SendMessageStream(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error {
	acc := NewToolUseAccumulator()
	isFirst := true
	err := b.streamEvents(ctx, messages, tools, func(ev StreamEvent) {
		acc.HandleEvent(ev)
		switch ev.Type {
		case EventContentDelta:
			callback(StreamChunk{IsFirst: isFirst, Content: ev.ContentDelta})
			isFirst = false
		case EventMessageDone:
			if isFirst {
				callback(StreamChunk{IsFirst: true})
				isFirst = false
			}
			callback(StreamChunk{
				IsFinal:      true,
				FinishReason: ev.FinishReason,
				ToolCalls:    acc.CompletedCalls(),
				Usage:        nil, // Google GenAI SDK doesn't provide token usage in streaming yet
			})
		}
	})
	if err != nil {
		return fmt.Errorf("Google AI stream error: %w", err)
	}
	return nil
}

// SendMessageStreamEvents sends a message with granular streaming events.
func (b *GoogleBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	if err := b.streamEvents(ctx, messages, tools, callback); err != nil {
		return fmt.Errorf("Google AI stream error: %w", err)
	}
	return nil
}

// streamEvents is the backend's one loop over GenerateContentStream: each
// chunk counts as activity on the stall watch, thoughts become thinking,
// text becomes content, function calls (sent complete) become a start and
// a done event, and EventMessageDone carries the finish reason. It returns
// the SDK's error unwrapped, for the caller to label.
func (b *GoogleBackend) streamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	contents, genConfig := b.request(messages, tools)
	var lastFinishReason string
	for chunk, err := range b.client.Models.GenerateContentStream(ctx, b.config.Model, contents, genConfig) {
		if err != nil {
			return err
		}
		touchStall(ctx)

		for _, candidate := range chunk.Candidates {
			if candidate.Content != nil {
				// Thoughts feed the thinking indicator, never the reply.
				if thought := extractThoughts(candidate.Content); thought != "" {
					callback(StreamEvent{
						Type:          EventThinkingDelta,
						ThinkingDelta: thought,
					})
				}

				// Extract text content
				text := extractText(candidate.Content)
				if text != "" {
					callback(StreamEvent{
						Type:         EventContentDelta,
						ContentDelta: text,
					})
				}

				// Extract function calls — Google sends them complete
				for _, part := range candidate.Content.Parts {
					if part.FunctionCall != nil {
						toolCall := b.convertFunctionCallToResult(part.FunctionCall, part.ThoughtSignature)
						// Emit start then immediately done. The signature must
						// ride on the events: the accumulator rebuilds
						// ToolCallResult from these alone, so anything left on
						// toolCall here is dropped and the next turn 400s.
						callback(StreamEvent{
							Type:             EventToolUseStart,
							ToolUseID:        toolCall.ID,
							ToolName:         toolCall.Name,
							ThoughtSignature: toolCall.ThoughtSignature,
						})
						callback(StreamEvent{
							Type:             EventToolUseDone,
							ToolUseID:        toolCall.ID,
							ToolName:         toolCall.Name,
							CompleteInput:    toolCall.Arguments,
							ThoughtSignature: toolCall.ThoughtSignature,
						})
					}
				}
			}

			if candidate.FinishReason != "" {
				lastFinishReason = string(candidate.FinishReason)
			}
		}
	}

	// Emit MessageDone
	if lastFinishReason == "" {
		lastFinishReason = "stop"
	}
	callback(StreamEvent{
		Type:         EventMessageDone,
		Usage:        nil, // Google GenAI SDK doesn't provide token usage in streaming yet
		FinishReason: lastFinishReason,
	})

	return nil
}

// applyThinkingConfig adds ThinkingConfig to the generation config when
// thinking is enabled.
func (b *GoogleBackend) applyThinkingConfig(genConfig *genai.GenerateContentConfig) {
	thinking := b.thinking()
	if !thinking.Enabled || thinking.Level == "off" {
		return
	}
	budget := thinking.LevelToBudget()
	tc := &genai.ThinkingConfig{
		IncludeThoughts: true,
	}
	if budget > 0 {
		b32 := int32(budget)
		tc.ThinkingBudget = &b32
	}
	genConfig.ThinkingConfig = tc
}

// Close cleans up resources.
func (b *GoogleBackend) Close() error {
	// Google GenAI SDK client doesn't require explicit cleanup
	return nil
}

// convertMessagesToGenAI converts Celeste messages to Google GenAI format.
func (b *GoogleBackend) convertMessagesToGenAI(messages []tui.ChatMessage) []*genai.Content {
	var contents []*genai.Content

	// A function response names the function it answers: find it by the
	// call ID among the assistant's calls.
	callNames := map[string]string{}
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" && tc.Name != "" {
				callNames[tc.ID] = tc.Name
			}
		}
	}

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
			name := callNames[msg.ToolCallID]
			if name == "" {
				name = msg.Name
			}
			if name == "" {
				name = msg.ToolCallID // no call to pair it with: as before
			}
			part := genai.NewPartFromFunctionResponse(name, map[string]any{
				"result": msg.Content,
			})

			// Function responses use "user" role in Google GenAI
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{part}, genai.RoleUser))

			// If the tool result carries image metadata, inject a user
			// message with the image as inline data so Gemini can see it.
			if img, note, ok := fitToolImage(msg.Metadata, imagefit.Gemini); ok && note != "" {
				contents = append(contents, genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(note)}, genai.RoleUser))
			} else if ok {
				if imageBytes, err := base64.StdEncoding.DecodeString(img.B64); err == nil {
					parts := []*genai.Part{
						genai.NewPartFromText(fmt.Sprintf("[Attached image from tool result: %s]", img.Name)),
						genai.NewPartFromBytes(imageBytes, img.MediaType()),
					}
					contents = append(contents, genai.NewContentFromParts(parts, genai.RoleUser))
				}
			}
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

				part := genai.NewPartFromFunctionCall(tc.Name, args)
				// Gemini 3.x rejects the follow-up turn with "Function call is
				// missing a thought_signature" unless the signature that came
				// back with the functionCall is echoed here verbatim.
				part.ThoughtSignature = tc.ThoughtSignature
				parts = append(parts, part)
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
	switch required := params["required"].(type) {
	case []string:
		schema.Required = required
	case []interface{}:
		requiredStrs := make([]string, 0, len(required))
		for _, v := range required {
			if str, ok := v.(string); ok && str != "" {
				requiredStrs = append(requiredStrs, str)
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

// convertFunctionCallToResult converts a Google function-call part to our
// ToolCallResult format. The signature comes from the enclosing Part, not the
// FunctionCall, so callers pass part.ThoughtSignature alongside it.
func (b *GoogleBackend) convertFunctionCallToResult(fc *genai.FunctionCall, thoughtSignature []byte) ToolCallResult {
	// Keep an ID the API supplies; otherwise make one unique per call.
	// "call_<name>" alone repeats for every call of a tool, and /rewind
	// maps file changes to turns by call ID (2.0 W4). The response is
	// paired with its call by name (convertMessagesToGenAI), not by ID.
	toolCallID := fc.ID
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%s_%s", fc.Name, config.UniqueNanoID())
	}

	// Convert arguments to JSON string
	argsJSON := "{}"
	if fc.Args != nil {
		// Marshal the args map to JSON
		if jsonBytes, err := json.Marshal(fc.Args); err == nil {
			argsJSON = string(jsonBytes)
		}
	}

	return ToolCallResult{
		ID:               toolCallID,
		Name:             fc.Name,
		Arguments:        argsJSON,
		ThoughtSignature: thoughtSignature,
	}
}

// extractText extracts the reply text from a Google GenAI Content object.
// Thought parts (IncludeThoughts) are the model's reasoning, not reply
// text: they are left out here and read with extractThoughts.
func extractText(content *genai.Content) string {
	var text strings.Builder

	for _, part := range content.Parts {
		if part.Text != "" && !part.Thought {
			text.WriteString(part.Text)
		}
	}

	return text.String()
}

// extractThoughts extracts the thought-summary text from a Google GenAI
// Content object.
func extractThoughts(content *genai.Content) string {
	var text strings.Builder

	for _, part := range content.Parts {
		if part.Text != "" && part.Thought {
			text.WriteString(part.Text)
		}
	}

	return text.String()
}
