// Package commands handles slash commands for Celeste CLI.
// Commands provide direct user control over modes, endpoints, and configuration.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// Command represents a parsed slash command.
type Command struct {
	Name string
	Args []string
	Raw  string
}

// CommandContext provides context for command execution.
type CommandContext struct {
	NSFWMode      bool
	Provider      string // Current provider (grok, openai, venice, etc.)
	CurrentModel  string // Current model in use
	APIKey        string // API key for model listing
	BaseURL       string // Base URL for API calls
	SkillsEnabled bool   // Whether skills/functions are currently enabled
	Version       string // Application version
	Build         string // Build identifier
}

// CommandResult represents the result of executing a command.
type CommandResult struct {
	Success      bool
	Message      string
	ShouldRender bool // Whether to show in chat history
	StateChange  *StateChange
}

// SelectorItem represents an item in the interactive selector.
type SelectorItem struct {
	ID          string
	DisplayName string
	Description string
	Badge       string
}

// SelectorData holds data for showing the interactive selector.
type SelectorData struct {
	Title string
	Items []SelectorItem
}

// StateChange represents a change in application state.
type StateChange struct {
	EndpointChange *string
	NSFWMode       *bool
	Model          *string
	ImageModel     *string
	ClearHistory   bool
	MenuState      *string        // "status", "commands", "skills"
	SessionAction  *SessionAction // Session management operations
	ShowSelector   *SelectorData  // Show interactive selector
}

// SessionAction represents a session management operation.
type SessionAction struct {
	Action    string // "new", "resume", "list", "clear", "merge", "info"
	SessionID string // For resume/merge operations
	Name      string // For new session with name
}

// Parse parses a message to check if it's a command.
// Returns nil if not a command.
func Parse(input string) *Command {
	input = strings.TrimSpace(input)

	if !strings.HasPrefix(input, "/") {
		return nil
	}

	// Split by whitespace
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	cmd := &Command{
		Name: strings.TrimPrefix(parts[0], "/"),
		Raw:  input,
	}

	if len(parts) > 1 {
		cmd.Args = parts[1:]
	}

	return cmd
}

// Execute executes a command and returns the result.
func Execute(cmd *Command, ctx *CommandContext) *CommandResult {
	if ctx == nil {
		ctx = &CommandContext{}
	}

	switch strings.ToLower(cmd.Name) {
	case "nsfw":
		return handleNSFW(cmd)
	case "safe":
		return handleSafe(cmd)
	case "endpoint":
		return handleEndpoint(cmd)
	case "model":
		return handleModel(cmd)
	case "image-model", "set-model", "list-models":
		return handleSetModel(cmd, ctx)
	case "config":
		return handleConfig(cmd)
	case "clear":
		return handleClear(cmd)
	case "help":
		return handleHelp(cmd, ctx)
	case "menu":
		return handleMenu(cmd)
	case "skills":
		return handleSkills(cmd, ctx)
	case "providers":
		return HandleProvidersCommand(cmd, ctx)
	case "session":
		return handleSession(cmd, ctx)
	case "context":
		// Note: HandleContextCommand requires contextTracker from app state
		// This will be called from app.go with proper context
		return &CommandResult{
			Success:      false,
			Message:      "⚠️ /context command requires app context - this should be handled by the TUI",
			ShouldRender: true,
		}
	case "stats":
		// Note: HandleStatsCommand requires contextTracker from app state
		// This will be called from app.go with proper context
		return &CommandResult{
			Success:      false,
			Message:      "⚠️ /stats command requires app context - this should be handled by the TUI",
			ShouldRender: true,
		}
	case "export":
		// Note: HandleExportCommand requires currentSession from app state
		// This will be called from app.go with proper context
		return &CommandResult{
			Success:      false,
			Message:      "⚠️ /export command requires app context - this should be handled by the TUI",
			ShouldRender: true,
		}
	default:
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", cmd.Name),
			ShouldRender: true,
		}
	}
}

// handleNSFW handles the /nsfw command.
func handleNSFW(cmd *Command) *CommandResult {
	enabled := true
	defaultImageModel := "lustify-sdxl"
	return &CommandResult{
		Success:      true,
		Message:      "🔥 NSFW Mode Enabled\n\nSwitched to Venice.ai endpoint for uncensored content.\nImage Model: lustify-sdxl\n\nUse /set-model <model> to change image model.\nUse /help to see available models and commands.",
		ShouldRender: true,
		StateChange: &StateChange{
			NSFWMode:   &enabled,
			ImageModel: &defaultImageModel,
		},
	}
}

// handleSafe handles the /safe command.
func handleSafe(cmd *Command) *CommandResult {
	disabled := false
	return &CommandResult{
		Success:      true,
		Message:      "✅ Safe Mode Enabled\n\nReturned to your default endpoint.\nSkills/function calling will be re-enabled if supported by the model.",
		ShouldRender: true,
		StateChange: &StateChange{
			NSFWMode: &disabled,
		},
	}
}

// handleEndpoint handles the /endpoint command.
func handleEndpoint(cmd *Command) *CommandResult {
	if len(cmd.Args) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "Usage: /endpoint <name>\n\nAvailable endpoints:\n  • openai\n  • venice\n  • grok\n  • elevenlabs\n  • google (for Vertex AI)\n\nExample: /endpoint venice",
			ShouldRender: true,
		}
	}

	endpoint := strings.ToLower(cmd.Args[0])
	validEndpoints := map[string]string{
		"openai":     "OpenAI",
		"venice":     "Venice.ai",
		"grok":       "xAI Grok",
		"elevenlabs": "ElevenLabs",
		"google":     "Google Vertex AI",
	}

	if displayName, ok := validEndpoints[endpoint]; ok {
		return &CommandResult{
			Success:      true,
			Message:      fmt.Sprintf("🔄 Switched to %s\n\nAll requests will use this endpoint until changed.", displayName),
			ShouldRender: true,
			StateChange: &StateChange{
				EndpointChange: &endpoint,
			},
		}
	}

	return &CommandResult{
		Success:      false,
		Message:      fmt.Sprintf("Unknown endpoint: %s\n\nAvailable: openai, venice, grok, elevenlabs, google", endpoint),
		ShouldRender: true,
	}
}

// handleModel handles the /model command.
func handleModel(cmd *Command) *CommandResult {
	if len(cmd.Args) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "Usage: /model <name>\n\nCommon models:\n  • gpt-4o-mini\n  • gpt-4o\n  • claude-3-5-sonnet\n  • llama-3.3-70b\n\nExample: /model gpt-4o",
			ShouldRender: true,
		}
	}

	model := strings.Join(cmd.Args, " ")
	return &CommandResult{
		Success:      true,
		Message:      fmt.Sprintf("🤖 Model changed to: %s", model),
		ShouldRender: true,
		StateChange: &StateChange{
			Model: &model,
		},
	}
}

// handleSetModel handles the /set-model and /list-models commands.
// Context-aware: image models in NSFW mode, chat models otherwise.
func handleSetModel(cmd *Command, ctx *CommandContext) *CommandResult {
	// NSFW mode: Handle image models (backward compatibility with Venice pattern)
	if ctx.NSFWMode {
		return handleImageModel(cmd, ctx)
	}

	// Chat mode: Handle chat models with provider awareness
	return handleChatModel(cmd, ctx)
}

// handleImageModel handles image model selection in NSFW mode (Venice pattern).
func handleImageModel(cmd *Command, ctx *CommandContext) *CommandResult {
	if len(cmd.Args) == 0 || cmd.Name == "list-models" {
		return &CommandResult{
			Success:      false,
			Message:      "Available Image Models:\n\n  • lustify-sdxl (default NSFW)\n  • wai-Illustrious (anime)\n  • hidream (dream-like)\n  • nano-banana-pro\n  • venice-sd35 (Stable Diffusion 3.5)\n  • lustify-v7\n\nUsage: /set-model <model-name>\nExample: /set-model wai-Illustrious\n\nOr use shortcuts: anime:, dream:, image:",
			ShouldRender: true,
		}
	}

	imageModel := cmd.Args[0]

	// Validate model name
	validModels := map[string]string{
		"lustify-sdxl":    "NSFW image generation",
		"wai-illustrious": "Anime style",
		"hidream":         "Dream-like quality",
		"nano-banana-pro": "Alternative model",
		"venice-sd35":     "Stable Diffusion 3.5",
		"lustify-v7":      "Lustify v7",
		"qwen-image":      "Qwen vision model",
	}

	modelLower := strings.ToLower(imageModel)
	if desc, ok := validModels[modelLower]; ok {
		return &CommandResult{
			Success:      true,
			Message:      fmt.Sprintf("🎨 Image model changed to: %s\n%s\n\nThis will be used for all image: prompts until changed.", imageModel, desc),
			ShouldRender: true,
			StateChange: &StateChange{
				ImageModel: &imageModel,
			},
		}
	}

	return &CommandResult{
		Success:      false,
		Message:      fmt.Sprintf("Unknown model: %s\n\nUse /set-model without arguments to see available models.", imageModel),
		ShouldRender: true,
	}
}

// handleChatModel handles chat model selection with provider capabilities.
func handleChatModel(cmd *Command, ctx *CommandContext) *CommandResult {
	// Get provider capabilities
	caps, ok := providers.GetProvider(ctx.Provider)
	if !ok {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("Unknown provider: %s\n\nUse /endpoint to switch providers.", ctx.Provider),
			ShouldRender: true,
		}
	}

	// No args or /list-models: Show available models
	if len(cmd.Args) == 0 || cmd.Name == "list-models" {
		return listAvailableModels(ctx, caps)
	}

	// Check for --force flag
	forceModel := false
	modelName := cmd.Args[0]
	if len(cmd.Args) > 1 && cmd.Args[1] == "--force" {
		forceModel = true
	}

	// Create model service to validate
	modelService := providers.NewModelService(ctx.APIKey, ctx.BaseURL, ctx.Provider)
	modelInfo, err := modelService.ValidateModel(context.Background(), modelName)

	if err != nil {
		// Model not found, but allow if --force
		if forceModel {
			return &CommandResult{
				Success:      true,
				Message:      fmt.Sprintf("🤖 Model changed to: %s\n⚠️  Model validation unavailable", modelName),
				ShouldRender: true,
				StateChange: &StateChange{
					Model: &modelName,
				},
			}
		}

		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Model '%s' not found for provider %s\n\nUse /set-model to see available models.\nUse /set-model %s --force to set anyway.", modelName, caps.Name, modelName),
			ShouldRender: true,
		}
	}

	// Model found - check tool support
	if !modelInfo.SupportsTools && ctx.SkillsEnabled {
		if !forceModel {
			return &CommandResult{
				Success:      false,
				Message:      fmt.Sprintf("⚠️  Model '%s' does not support function calling.\n\n%s\n\nSkills will be disabled with this model.\n\n✓ Use /set-model %s for skills support\n  Or proceed with /set-model %s --force", modelName, modelInfo.Description, caps.PreferredToolModel, modelName),
				ShouldRender: true,
			}
		}

		// Forced non-tool model
		return &CommandResult{
			Success:      true,
			Message:      fmt.Sprintf("🤖 Model changed to: %s\n⚠️  Skills disabled - model does not support function calling\n\n%s", modelName, modelInfo.Description),
			ShouldRender: true,
			StateChange: &StateChange{
				Model: &modelName,
			},
		}
	}

	// Model supports tools or skills aren't required
	checkmark := ""
	if modelInfo.SupportsTools {
		checkmark = " ✓"
	}

	return &CommandResult{
		Success:      true,
		Message:      fmt.Sprintf("🤖 Model changed to: %s%s\n\n%s", modelName, checkmark, modelInfo.Description),
		ShouldRender: true,
		StateChange: &StateChange{
			Model: &modelName,
		},
	}
}

// listAvailableModels fetches and displays available models for current provider.
func listAvailableModels(ctx *CommandContext, caps providers.ProviderCapabilities) *CommandResult {
	modelService := providers.NewModelService(ctx.APIKey, ctx.BaseURL, ctx.Provider)

	models, err := modelService.ListModels(context.Background())
	if err != nil {
		// Fallback to common models help
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("Failed to fetch models from %s\n\n%s\n\nCommon models:\n%s\n\nUsage: /set-model <model-id>", caps.Name, err, getCommonModelsHelp(ctx.Provider)),
			ShouldRender: true,
		}
	}

	// Convert models to selector items
	selectorItems := make([]SelectorItem, len(models))
	for i, model := range models {
		badge := ""
		if model.SupportsTools {
			badge = "✓"
		}

		selectorItems[i] = SelectorItem{
			ID:          model.ID,
			DisplayName: model.Name,
			Description: model.Description,
			Badge:       badge,
		}
	}

	// Create selector data
	title := fmt.Sprintf("📋 Available Models for %s", caps.Name)
	if caps.PreferredToolModel != "" {
		title += fmt.Sprintf(" (💡 Recommended: %s)", caps.PreferredToolModel)
	}

	return &CommandResult{
		Success:      true,
		Message:      "", // No message, using selector instead
		ShouldRender: false,
		StateChange: &StateChange{
			ShowSelector: &SelectorData{
				Title: title,
				Items: selectorItems,
			},
		},
	}
}

// getCommonModelsHelp returns static model suggestions when API fails.
func getCommonModelsHelp(provider string) string {
	switch provider {
	case "grok":
		return "  • grok-4-1-fast (recommended for skills)\n  • grok-4-1\n  • grok-beta"
	case "openai":
		return "  • gpt-4o-mini (recommended)\n  • gpt-4o\n  • gpt-4-turbo"
	case "venice":
		return "  • venice-uncensored (no skills)\n  • llama-3.3-70b\n  • qwen3-235b"
	case "anthropic":
		return "  • claude-sonnet-4-5-20250929\n  • claude-opus-4-5-20251101"
	case "vertex":
		return "  • gemini-1.5-pro\n  • gemini-1.5-flash"
	case "openrouter":
		return "  • openai/gpt-4o-mini\n  • anthropic/claude-sonnet-4-5"
	default:
		return "  (provider-specific models)"
	}
}

// handleConfig handles the /config command.
func handleConfig(cmd *Command) *CommandResult {
	// No args: List available configs
	if len(cmd.Args) == 0 {
		return listAvailableConfigs()
	}

	configName := cmd.Args[0]
	return &CommandResult{
		Success:      true,
		Message:      fmt.Sprintf("⚙️  Loaded config profile: %s", configName),
		ShouldRender: true,
		StateChange: &StateChange{
			EndpointChange: &configName,
		},
	}
}

// listAvailableConfigs lists all available configuration profiles.
func listAvailableConfigs() *CommandResult {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".celeste")

	// Read directory
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Error reading config directory: %v\n\nConfig directory: %s", err, configDir),
			ShouldRender: true,
		}
	}

	// Find all config files
	configs := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "config.") && strings.HasSuffix(name, ".json") {
			// Extract profile name: config.grok.json -> grok
			profileName := strings.TrimPrefix(name, "config.")
			profileName = strings.TrimSuffix(profileName, ".json")
			if profileName != "" {
				configs = append(configs, profileName)
			}
		} else if name == "config.json" {
			configs = append(configs, "default")
		}
	}

	if len(configs) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ No configuration profiles found.\n\nCreate configs in: ~/.celeste/\n\nExample:\n  config.json (default)\n  config.grok.json\n  config.vertex.json",
			ShouldRender: true,
		}
	}

	// Build message
	var msg strings.Builder
	msg.WriteString("⚙️  Available Configuration Profiles:\n\n")

	for _, profile := range configs {
		// Load config to show details
		var configPath string
		if profile == "default" {
			configPath = filepath.Join(configDir, "config.json")
		} else {
			configPath = filepath.Join(configDir, fmt.Sprintf("config.%s.json", profile))
		}

		// Read config
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		// Extract key info
		baseURL := ""
		model := ""
		if val, ok := cfg["base_url"].(string); ok {
			baseURL = val
		}
		if val, ok := cfg["model"].(string); ok {
			model = val
		}

		// Detect provider using providers.DetectProvider
		providerKey := providers.DetectProvider(baseURL)
		providerCaps, found := providers.GetProvider(providerKey)

		// Get display name
		provider := "unknown"
		if found {
			provider = providerCaps.Name
		} else {
			// Fallback to manual detection if not in registry
			switch {
			case strings.Contains(baseURL, "openai.com"):
				provider = "OpenAI"
			case strings.Contains(baseURL, "x.ai"):
				provider = "xAI Grok"
			case strings.Contains(baseURL, "venice.ai"):
				provider = "Venice.ai"
			case strings.Contains(baseURL, "anthropic.com"):
				provider = "Anthropic"
			case strings.Contains(baseURL, "generativelanguage.googleapis.com"):
				provider = "Google Gemini AI"
			case strings.Contains(baseURL, "aiplatform.googleapis.com"):
				provider = "Google Vertex AI"
			case strings.Contains(baseURL, "openrouter.ai"):
				provider = "OpenRouter"
			case strings.Contains(baseURL, "digitalocean"):
				provider = "DigitalOcean"
			}
		}

		// Check if provider supports function calling
		indicator := ""
		if providerCaps.SupportsFunctionCalling {
			indicator = " ✓"
		} else if provider != "unknown" {
			indicator = " ⚠️"
		}

		msg.WriteString(fmt.Sprintf("  • %s%s\n", profile, indicator))
		msg.WriteString(fmt.Sprintf("    Provider: %s\n", provider))
		if model != "" {
			msg.WriteString(fmt.Sprintf("    Model: %s\n", model))
		}
		msg.WriteString("\n")
	}

	msg.WriteString("Usage: /config <profile-name>\n")
	msg.WriteString("Example: /config grok\n\n")
	msg.WriteString("Legend:\n")
	msg.WriteString("  ✓  = Function calling supported (skills available)\n")
	msg.WriteString("  ⚠️  = No function calling (skills unavailable)\n")

	return &CommandResult{
		Success:      true,
		Message:      msg.String(),
		ShouldRender: true,
	}
}

// handleClear handles the /clear command.
func handleClear(cmd *Command) *CommandResult {
	return &CommandResult{
		Success:      true,
		Message:      "🗑️  Conversation cleared",
		ShouldRender: false,
		StateChange: &StateChange{
			ClearHistory: true,
		},
	}
}

// handleHelp handles the /help command.
func handleHelp(cmd *Command, ctx *CommandContext) *CommandResult {
	var helpText string

	// Version header
	versionHeader := ""
	if ctx.Version != "" {
		versionHeader = fmt.Sprintf("Celeste CLI v%s", ctx.Version)
		if ctx.Build != "" {
			versionHeader += fmt.Sprintf(" (%s)", ctx.Build)
		}
		versionHeader += "\n\n"
	}

	if ctx.NSFWMode {
		// NSFW Mode Help
		helpText = versionHeader + `🔥 NSFW Mode - Venice.ai Uncensored

Media Generation Commands:
  image: <prompt>              Generate images with current model
                               Example: image: cyberpunk cityscape at night

  anime: <prompt>              Generate anime-style images (wai-Illustrious)
                               Example: anime: magical girl with sword

  dream: <prompt>              High-quality dream-like images (hidream)
                               Example: dream: surreal landscape

  image[model]: <prompt>       Use specific model for one generation
                               Example: image[nano-banana-pro]: futuristic city

  upscale: <path>              Upscale and enhance existing image
                               Example: upscale: ~/photo.jpg

Model Management:
  /set-model <model>           Set default image generation model
                               Example: /set-model wai-Illustrious
                               Run without args to see all models

Chat Commands:
  /safe                        Return to safe mode (OpenAI)
  /clear                       Clear conversation history
  /help                        Show this help message

Current Configuration:
  • Endpoint: Venice.ai (https://api.venice.ai/api/v1)
  • Chat Model: venice-uncensored (no function calling)
  • Image Model: Use /set-model to configure
  • Downloads: ~/Downloads
  • Quality: 40 steps, CFG 12.0, PNG format

Available Image Models:
  • lustify-sdxl - NSFW image generation (default)
  • wai-Illustrious - Anime style
  • hidream - Dream-like quality
  • nano-banana-pro - Alternative model
  • venice-sd35 - Stable Diffusion 3.5
  • lustify-v7 - Lustify v7
  • qwen-image - Qwen vision model

Image Quality Parameters (defaults):
  • Steps: 40 (1-50, higher = more detail)
  • CFG Scale: 12.0 (0-20, higher = stronger prompt adherence)
  • Size: 1024x1024 (up to 1280x1280)
  • Format: PNG (lossless)
  • Safe Mode: Disabled (no NSFW blurring)

Configure downloads_dir in ~/.celeste/skills.json to change save location.

Tip: Ask the uncensored LLM to write detailed NSFW prompts, then use
"image: [paste prompt]" to generate from that description!`
	} else {
		// Safe Mode Help
		helpText = versionHeader + `Available Commands:

Mode Control:
  /nsfw              Switch to NSFW mode (Venice.ai, uncensored)
  /safe              Switch to safe mode (OpenAI, content policy)

Endpoint Control:
  /endpoint <name>   Switch to a specific endpoint
                     Options: openai, venice, grok, elevenlabs, google
  /config <name>     Load a named config profile
  /model <name>      Change the model (e.g., gpt-4o, llama-3.3-70b)

Session Control:
  /clear             Clear conversation history
  /help              Show this help message

Examples:
  /nsfw              → Enable uncensored mode with media generation
  /endpoint google   → Switch to Google Vertex AI
  /model gpt-4o      → Use GPT-4o model
  /safe              → Return to safe mode

Skills Available: 18 function-calling tools
  • Weather, currency, timezone conversion
  • Hashing, encoding, UUID generation
  • Twitch live checks, YouTube videos
  • Reminders, notes, tarot readings
  • QR codes, passwords

Tip: You can also add keywords like "nsfw" or "uncensored" at the end
of your message for automatic routing while staying in control.`
	}

	return &CommandResult{
		Success:      true,
		Message:      helpText,
		ShouldRender: true,
	}
}

// DetectRoutingHints checks if message contains routing hints.
// Returns suggested endpoint or empty string.
func DetectRoutingHints(message string) string {
	lower := strings.ToLower(message)

	// Check for explicit routing hints
	hints := map[string]string{
		"#nsfw":       "venice",
		"#uncensored": "venice",
		"#venice":     "venice",
		"#explicit":   "venice",
		"#mature":     "venice",
	}

	for hint, endpoint := range hints {
		if strings.Contains(lower, hint) {
			return endpoint
		}
	}

	// Check for contextual hints at end of message
	words := strings.Fields(message)
	if len(words) > 0 {
		lastWord := strings.ToLower(words[len(words)-1])
		contextHints := map[string]string{
			"nsfw":       "venice",
			"uncensored": "venice",
			"explicit":   "venice",
			"lewd":       "venice",
			"mature":     "venice",
		}

		if endpoint, ok := contextHints[lastWord]; ok {
			return endpoint
		}
	}

	return ""
}

// IsImageGenerationRequest checks if the message is requesting image generation.
func IsImageGenerationRequest(message string) bool {
	lower := strings.ToLower(message)

	imageKeywords := []string{
		"generate an image",
		"generate image",
		"create an image",
		"create image",
		"make an image",
		"make image",
		"draw",
		"generate a picture",
		"create a picture",
		"generate art",
		"create art",
	}

	for _, keyword := range imageKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	return false
}

// IsContentPolicyRefusal checks if the LLM response is a content policy refusal.
func IsContentPolicyRefusal(response string) bool {
	lower := strings.ToLower(response)

	refusalPatterns := []string{
		"i can't",
		"i cannot",
		"i'm not able to",
		"i'm unable to",
		"against my",
		"content policy",
		"usage policy",
		"i don't feel comfortable",
		"inappropriate",
		"i'm designed to be helpful, harmless, and honest",
	}

	for _, pattern := range refusalPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// handleMenu handles the /menu command (toggle commands menu).
func handleMenu(cmd *Command) *CommandResult {
	menuState := "commands"
	return &CommandResult{
		Success:      true,
		Message:      "", // Don't render message - just change state
		ShouldRender: false,
		StateChange: &StateChange{
			MenuState: &menuState,
		},
	}
}

// handleSkills handles the /skills command (toggle skills menu).
func handleSkills(cmd *Command, ctx *CommandContext) *CommandResult {
	// If no arguments, show skills menu
	if len(cmd.Args) == 0 {
		menuState := "skills"
		return &CommandResult{
			Success:      true,
			Message:      "", // Don't render message - just change state
			ShouldRender: false,
			StateChange: &StateChange{
				MenuState: &menuState,
			},
		}
	}

	// Handle subcommands
	subcommand := strings.ToLower(cmd.Args[0])

	switch subcommand {
	case "list":
		return handleSkillsList()
	case "delete":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /skills delete <skill_name>",
				ShouldRender: true,
			}
		}
		return handleSkillsDelete(cmd.Args[1])
	case "info":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /skills info <skill_name>",
				ShouldRender: true,
			}
		}
		return handleSkillsInfo(cmd.Args[1])
	case "reload":
		return handleSkillsReload()
	default:
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("Unknown /skills subcommand: %s\n\nAvailable: list, delete <name>, info <name>, reload", subcommand),
			ShouldRender: true,
		}
	}
}

// handleSkillsList shows all registered skills with count
func handleSkillsList() *CommandResult {
	return &CommandResult{
		Success:      false,
		Message:      "⚠️ /skills list requires app context - this should be handled by the TUI",
		ShouldRender: true,
	}
}

// handleSkillsDelete removes a skill from the registry
func handleSkillsDelete(name string) *CommandResult {
	return &CommandResult{
		Success:      false,
		Message:      fmt.Sprintf("⚠️ /skills delete requires app context - this should be handled by the TUI\n\nSkill to delete: %s", name),
		ShouldRender: true,
	}
}

// handleSkillsInfo shows detailed information about a skill
func handleSkillsInfo(name string) *CommandResult {
	return &CommandResult{
		Success:      false,
		Message:      fmt.Sprintf("⚠️ /skills info requires app context - this should be handled by the TUI\n\nSkill to query: %s", name),
		ShouldRender: true,
	}
}

// handleSkillsReload reloads skills from disk
func handleSkillsReload() *CommandResult {
	return &CommandResult{
		Success:      false,
		Message:      "⚠️ /skills reload requires app context - this should be handled by the TUI",
		ShouldRender: true,
	}
}

// handleSession handles the /session command for session management.
func handleSession(cmd *Command, ctx *CommandContext) *CommandResult {
	if len(cmd.Args) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "Usage: /session <action> [args]\n\nAvailable actions:\n  • new [name]         - Start a new session\n  • resume <id|name>   - Resume a previous session by ID or name\n  • list               - List all saved sessions\n  • clear              - Clear current session\n  • merge <id>         - Merge another session into current\n  • info               - Show current session statistics\n  • rename <id> <name> - Rename a session\n  • delete <id>        - Delete a session\n\nExamples:\n  /session new \"Planning notes\"\n  /session resume 1733609876123\n  /session resume \"Planning notes\"\n  /session rename 1733609876123 \"New name\"\n  /session delete 1733609876123\n  /session list",
			ShouldRender: true,
		}
	}

	action := strings.ToLower(cmd.Args[0])

	switch action {
	case "new":
		name := ""
		if len(cmd.Args) > 1 {
			name = strings.Join(cmd.Args[1:], " ")
		}
		return &CommandResult{
			Success:      true,
			Message:      "📝 Creating new session...",
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action: "new",
					Name:   name,
				},
			},
		}

	case "resume":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /session resume <session-id>\n\nUse /session list to see available sessions.",
				ShouldRender: true,
			}
		}
		return &CommandResult{
			Success:      true,
			Message:      fmt.Sprintf("📂 Loading session %s...", cmd.Args[1]),
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action:    "resume",
					SessionID: cmd.Args[1],
				},
			},
		}

	case "list":
		return &CommandResult{
			Success:      true,
			Message:      "", // Will be populated by handler
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action: "list",
				},
			},
		}

	case "clear":
		return &CommandResult{
			Success:      true,
			Message:      "🗑️  Clearing current session...",
			ShouldRender: true,
			StateChange: &StateChange{
				ClearHistory: true,
				SessionAction: &SessionAction{
					Action: "clear",
				},
			},
		}

	case "merge":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /session merge <session-id>\n\nThis will merge the specified session into the current one chronologically.\nUse /session list to see available sessions.",
				ShouldRender: true,
			}
		}
		return &CommandResult{
			Success:      true,
			Message:      fmt.Sprintf("🔀 Merging session %s...", cmd.Args[1]),
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action:    "merge",
					SessionID: cmd.Args[1],
				},
			},
		}

	case "info":
		return &CommandResult{
			Success:      true,
			Message:      "", // Will be populated by handler
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action: "info",
				},
			},
		}

	case "rename":
		if len(cmd.Args) < 3 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /session rename <session-id> <new-name>\n\nExample:\n  /session rename 1733609876123 \"My Session\"",
				ShouldRender: true,
			}
		}
		sessionID := cmd.Args[1]
		newName := strings.Join(cmd.Args[2:], " ")
		return &CommandResult{
			Success:      true,
			Message:      "", // Will be populated by handler
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action:    "rename",
					SessionID: sessionID,
					Name:      newName,
				},
			},
		}

	case "delete", "rm":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "Usage: /session delete <session-id>\n\nUse /session list to see available sessions.",
				ShouldRender: true,
			}
		}
		return &CommandResult{
			Success:      true,
			Message:      "", // Will be populated by handler
			ShouldRender: true,
			StateChange: &StateChange{
				SessionAction: &SessionAction{
					Action:    "delete",
					SessionID: cmd.Args[1],
				},
			},
		}

	default:
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("Unknown session action: %s\n\nAvailable actions: new, resume, list, clear, merge, info, rename, delete", action),
			ShouldRender: true,
		}
	}
}
