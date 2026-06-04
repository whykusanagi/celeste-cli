package commands

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// HandleProvidersCommand handles the /providers command and its subcommands.
// Usage:
//
//	/providers               - List all providers
//	/providers --tools       - Show only tool-capable providers
//	/providers info <name>   - Show detailed capabilities
//	/providers current       - Show current provider info
func HandleProvidersCommand(cmd *Command, ctx *CommandContext) *CommandResult {
	// Parse subcommand
	if len(cmd.Args) == 0 {
		return listAllProviders(ctx)
	}

	subcommand := cmd.Args[0]

	switch subcommand {
	case "--tools":
		return listToolProviders(ctx)
	case "info":
		if len(cmd.Args) < 2 {
			return &CommandResult{
				Success:      false,
				Message:      "❌ Usage: /providers info <provider_name>",
				ShouldRender: true,
			}
		}
		return showProviderInfo(cmd.Args[1], ctx)
	case "current":
		return showCurrentProvider(ctx)
	default:
		// Check if it's a provider name (for backwards compatibility with "/providers <name>")
		if _, ok := providers.GetProvider(subcommand); ok {
			return showProviderInfo(subcommand, ctx)
		}
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Unknown /providers subcommand: %s\n\nAvailable: --tools, info <name>, current", subcommand),
			ShouldRender: true,
		}
	}
}

// listAllProviders displays all registered providers with their capabilities
func listAllProviders(ctx *CommandContext) *CommandResult {
	var output strings.Builder

	// Corrupt header
	output.WriteString("═══════════════════════════════════════════════\n")
	output.WriteString("           可用的 AI PROVIDERS\n")
	output.WriteString("═══════════════════════════════════════════════\n\n")

	allProviders := providers.ListProviders()

	for _, name := range allProviders {
		caps, ok := providers.GetProvider(name)
		if !ok {
			continue
		}

		// Provider status indicator
		status := "✓"
		if ctx.Provider == name {
			status = "▶" // Current provider
		}

		// Tool support indicator
		toolSupport := "[NO TOOLS]"
		if caps.SupportsFunctionCalling {
			toolSupport = "[TOOLS]"
		}

		// Build provider line
		output.WriteString(fmt.Sprintf("%s %-15s %-12s", status, name, toolSupport))

		// Default/preferred model
		if caps.PreferredToolModel != "" {
			output.WriteString(fmt.Sprintf(" %s (preferred)", caps.PreferredToolModel))
		} else if caps.DefaultModel != "" {
			output.WriteString(fmt.Sprintf(" %s (default)", caps.DefaultModel))
		}

		// Special notes
		if caps.BaseURL != "" && strings.Contains(caps.BaseURL, "digitalocean") {
			output.WriteString(" [cloud-only]")
		} else if strings.Contains(name, "vertex") {
			output.WriteString(" [OAuth required]")
		} else if strings.Contains(name, "elevenlabs") {
			output.WriteString(" [voice]")
		} else if strings.Contains(name, "openrouter") {
			output.WriteString(" [aggregator]")
		}

		output.WriteString("\n")
	}

	// Current provider info
	if ctx.Provider != "" {
		output.WriteString(fmt.Sprintf("\nCurrent: %s", ctx.Provider))
		if caps, ok := providers.GetProvider(ctx.Provider); ok {
			if caps.SupportsFunctionCalling {
				output.WriteString(" (function calling enabled)")
			}
		}
		output.WriteString("\n")
	}

	output.WriteString("\nUse: /providers info <name> for details\n")

	return &CommandResult{
		Success:      true,
		Message:      output.String(),
		ShouldRender: true,
	}
}

// listToolProviders displays only providers that support function calling
func listToolProviders(ctx *CommandContext) *CommandResult {
	var output strings.Builder

	output.WriteString("═══════════════════════════════════════════════\n")
	output.WriteString("      TOOL-CAPABLE AI PROVIDERS\n")
	output.WriteString("═══════════════════════════════════════════════\n\n")

	toolProviders := providers.GetToolCallingProviders()

	if len(toolProviders) == 0 {
		output.WriteString("No providers with function calling support found.\n")
	} else {
		for _, name := range toolProviders {
			caps, ok := providers.GetProvider(name)
			if !ok {
				continue
			}

			// Current provider indicator
			status := " "
			if ctx.Provider == name {
				status = "▶"
			}

			output.WriteString(fmt.Sprintf("%s %-15s", status, name))

			// Show preferred tool model
			if caps.PreferredToolModel != "" {
				output.WriteString(fmt.Sprintf(" %s", caps.PreferredToolModel))
			} else if caps.DefaultModel != "" {
				output.WriteString(fmt.Sprintf(" %s", caps.DefaultModel))
			}

			output.WriteString("\n")
		}
	}

	output.WriteString(fmt.Sprintf("\nTotal: %d tool-capable providers\n", len(toolProviders)))

	return &CommandResult{
		Success:      true,
		Message:      output.String(),
		ShouldRender: true,
	}
}

// showProviderInfo displays detailed information about a specific provider
func showProviderInfo(name string, ctx *CommandContext) *CommandResult {
	caps, ok := providers.GetProvider(name)
	if !ok {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Provider '%s' not found.\n\nAvailable providers:\n%s", name, strings.Join(providers.ListProviders(), ", ")),
			ShouldRender: true,
		}
	}

	var output strings.Builder

	// Header
	output.WriteString("═══════════════════════════════════════════════\n")
	output.WriteString(fmt.Sprintf("           PROVIDER: %s\n", strings.ToUpper(name)))
	output.WriteString("═══════════════════════════════════════════════\n\n")

	// Current provider indicator
	if ctx.Provider == name {
		output.WriteString("▶ CURRENT PROVIDER\n\n")
	}

	// API Endpoint
	if caps.BaseURL != "" {
		output.WriteString(fmt.Sprintf("API Endpoint:  %s\n", caps.BaseURL))
	}

	// Capabilities
	output.WriteString("\nCAPABILITIES:\n")
	output.WriteString(fmt.Sprintf("  Function Calling:    %s\n", boolToStatus(caps.SupportsFunctionCalling)))
	output.WriteString(fmt.Sprintf("  Model Listing:       %s\n", boolToStatus(caps.SupportsModelListing)))
	output.WriteString(fmt.Sprintf("  Token Tracking:      %s\n", boolToStatus(caps.SupportsTokenTracking)))
	output.WriteString(fmt.Sprintf("  OpenAI Compatible:   %s\n", boolToStatus(caps.IsOpenAICompatible)))

	// Models
	output.WriteString("\nMODELS:\n")
	if caps.DefaultModel != "" {
		output.WriteString(fmt.Sprintf("  Default:          %s\n", caps.DefaultModel))
	}
	if caps.PreferredToolModel != "" {
		output.WriteString(fmt.Sprintf("  Preferred (Tool): %s\n", caps.PreferredToolModel))
	}

	// Authentication Requirements
	output.WriteString("\nAUTHENTICATION:\n")
	if caps.RequiresAPIKey {
		output.WriteString("  Required: API Key\n")
		switch name {
		case "openai":
			output.WriteString("  Get key: https://platform.openai.com/api-keys\n")
			output.WriteString("  Format: sk-...\n")
		case "grok":
			output.WriteString("  Get key: https://console.x.ai/\n")
			output.WriteString("  Format: xai-...\n")
		case "anthropic":
			output.WriteString("  Get key: https://console.anthropic.com/\n")
			output.WriteString("  Format: sk-ant-...\n")
		case "gemini":
			output.WriteString("  Get key: https://aistudio.google.com/\n")
			output.WriteString("  Format: Google AI Studio API key\n")
		case "venice":
			output.WriteString("  Get key: https://venice.ai/\n")
			output.WriteString("  Format: Venice API key\n")
		case "vertex":
			output.WriteString("  Method: OAuth 2.0\n")
			output.WriteString("  Requires: GCP project setup\n")
		case "openrouter":
			output.WriteString("  Get key: https://openrouter.ai/keys\n")
			output.WriteString("  Format: sk-or-...\n")
		case "digitalocean":
			output.WriteString("  Method: DigitalOcean API token\n")
			output.WriteString("  Requires: Deployed App Platform app\n")
		case "elevenlabs":
			output.WriteString("  Get key: https://elevenlabs.io/\n")
			output.WriteString("  Format: ElevenLabs API key\n")
		}
	}

	// Test Status
	output.WriteString("\nTEST STATUS:\n")
	switch name {
	case "openai":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: 🔜 Ready\n")
		output.WriteString("  Status: Gold standard, fully validated\n")
	case "grok":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: 🔜 Ready\n")
		output.WriteString("  Status: Fully tested, production ready\n")
	case "venice":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: 🔜 Ready\n")
		output.WriteString("  Status: Model-dependent tool support\n")
	case "anthropic":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: 🔜 Ready\n")
		output.WriteString("  Status: OpenAI mode limited, native API recommended\n")
	case "gemini", "vertex", "openrouter", "digitalocean", "elevenlabs":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: ❓ Needs API key\n")
		output.WriteString("  Status: Configured, pending live validation\n")
	}

	// Known limitations and features
	output.WriteString("\nKEY FEATURES & LIMITATIONS:\n")
	switch name {
	case "openai":
		output.WriteString("  • Gold standard for function calling\n")
		output.WriteString("  • Full streaming support\n")
		output.WriteString("  • Comprehensive token tracking\n")
		output.WriteString("  • Dynamic model listing\n")
	case "grok":
		output.WriteString("  • Large context window (grok-build-0.1)\n")
		output.WriteString("  • Fast response times\n")
		output.WriteString("  • Full OpenAI compatibility\n")
		output.WriteString("  • Recommended: grok-build-0.1 for tools\n")
	case "venice":
		output.WriteString("  • Uncensored models available\n")
		output.WriteString("  • venice-uncensored: NO function calling\n")
		output.WriteString("  • llama-3.3-70b: supports tools\n")
		output.WriteString("  • Privacy-focused provider\n")
	case "anthropic":
		output.WriteString("  • 200k context window\n")
		output.WriteString("  • OpenAI compatibility mode has limitations\n")
		output.WriteString("  • Native API recommended (not yet implemented)\n")
		output.WriteString("  • No dynamic model listing\n")
	case "gemini":
		output.WriteString("  • Free tier available\n")
		output.WriteString("  • Multi-modal capabilities\n")
		output.WriteString("  • OpenAI compatibility mode (untested)\n")
		output.WriteString("  • May require native Google AI SDK\n")
	case "vertex":
		output.WriteString("  • Enterprise GCP integration\n")
		output.WriteString("  • Requires OAuth setup\n")
		output.WriteString("  • Same models as Gemini\n")
		output.WriteString("  • More complex authentication\n")
	case "openrouter":
		output.WriteString("  • Access to 100+ models\n")
		output.WriteString("  • Model aggregator service\n")
		output.WriteString("  • Function calling varies by model\n")
		output.WriteString("  • Pricing varies by provider\n")
	case "digitalocean":
		output.WriteString("  • Cloud-hosted agents only\n")
		output.WriteString("  • Cannot use local Celeste skills\n")
		output.WriteString("  • Requires App Platform deployment\n")
		output.WriteString("  • Limited to gpt-4o-mini\n")
	case "elevenlabs":
		output.WriteString("  • Voice synthesis API\n")
		output.WriteString("  • Different use case (not chat)\n")
		output.WriteString("  • Function calling support unknown\n")
		output.WriteString("  • Requires voice-specific integration\n")
	default:
		output.WriteString("  • See provider documentation for details\n")
	}

	// Example Usage
	output.WriteString("\nEXAMPLE USAGE:\n")
	if caps.BaseURL != "" {
		output.WriteString("  # Configure via commands:\n")
		output.WriteString(fmt.Sprintf("  ./celeste config --set-url %s\n", caps.BaseURL))
	}
	if caps.DefaultModel != "" {
		output.WriteString(fmt.Sprintf("  ./celeste config --set-model %s\n", caps.DefaultModel))
	}
	output.WriteString("  ./celeste config --set-key YOUR_API_KEY\n")
	output.WriteString("\n  # Or edit ~/.celeste/config.json directly:\n")
	output.WriteString("  {\n")
	if caps.BaseURL != "" {
		output.WriteString(fmt.Sprintf("    \"base_url\": \"%s\",\n", caps.BaseURL))
	}
	if caps.DefaultModel != "" {
		output.WriteString(fmt.Sprintf("    \"model\": \"%s\",\n", caps.DefaultModel))
	}
	output.WriteString("    \"api_key\": \"YOUR_API_KEY\"\n")
	output.WriteString("  }\n")

	// Switching recommendation
	if name != ctx.Provider {
		output.WriteString("\n💡 To switch to this provider:\n")
		output.WriteString("   Use the config commands above, or see: ./celeste providers\n")
	}

	return &CommandResult{
		Success:      true,
		Message:      output.String(),
		ShouldRender: true,
	}
}

// showCurrentProvider displays information about the currently active provider
func showCurrentProvider(ctx *CommandContext) *CommandResult {
	if ctx.Provider == "" {
		return &CommandResult{
			Success:      true,
			Message:      "⚠️ No provider detected.\n\nProvider will be auto-detected from your BaseURL configuration.",
			ShouldRender: true,
		}
	}

	// Reuse the provider info function
	return showProviderInfo(ctx.Provider, ctx)
}

// Helper functions

func boolToStatus(b bool) string {
	if b {
		return "✓ Yes"
	}
	return "✗ No"
}
