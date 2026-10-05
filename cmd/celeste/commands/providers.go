package commands

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
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

		// Tool support indicator. A ToolsPerModel provider (Venice) shows
		// [PER MODEL]: the chat gates tools per selected model (#151 W6b).
		toolSupport := "[NO TOOLS]"
		if caps.SupportsFunctionCalling {
			toolSupport = "[TOOLS]"
		} else if caps.ToolsPerModel {
			toolSupport = "[PER MODEL]"
		}

		// Build provider line
		output.WriteString(fmt.Sprintf("%s %-15s %-12s", status, name, toolSupport))

		// Default/preferred model
		if caps.PreferredToolModel != "" {
			output.WriteString(fmt.Sprintf(" %s (preferred)", caps.PreferredToolModel))
		} else if caps.DefaultModel != "" {
			output.WriteString(fmt.Sprintf(" %s (default)", caps.DefaultModel))
		}
		if caps.DefaultModelUnverified {
			output.WriteString(" (unverified)")
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

	output.WriteString(fmt.Sprintf("\nTotal: %d providers: %d with tools, %d where it depends on the model\n",
		len(allProviders), len(providers.GetToolCallingProviders()), len(providers.GetPerModelToolProviders())))
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
			if caps.DefaultModelUnverified {
				output.WriteString(" (unverified)")
			}

			output.WriteString("\n")
		}
	}

	perModel := providers.GetPerModelToolProviders()
	if len(perModel) > 0 {
		output.WriteString("\nTools depend on the model (checked against the live catalogue):\n")
		for _, name := range perModel {
			output.WriteString(fmt.Sprintf("  %s\n", name))
		}
	}

	output.WriteString(fmt.Sprintf("\nTotal: %d tool-capable providers, plus %d where it depends on the model\n", len(toolProviders), len(perModel)))

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
	} else if caps.ExampleBaseURL != "" {
		output.WriteString(fmt.Sprintf("API Endpoint:  your own, for example %s\n", caps.ExampleBaseURL))
	}

	// Capabilities
	output.WriteString("\nCAPABILITIES:\n")
	if !caps.SupportsFunctionCalling && caps.ToolsPerModel {
		output.WriteString("  Function Calling:    ◐ Per model (checked against the live catalogue)\n")
	} else {
		output.WriteString(fmt.Sprintf("  Function Calling:    %s\n", boolToStatus(caps.SupportsFunctionCalling)))
	}
	output.WriteString(fmt.Sprintf("  Model Listing:       %s\n", boolToStatus(caps.SupportsModelListing)))
	output.WriteString(fmt.Sprintf("  Token Tracking:      %s\n", boolToStatus(caps.SupportsTokenTracking)))
	output.WriteString(fmt.Sprintf("  OpenAI Compatible:   %s\n", boolToStatus(caps.IsOpenAICompatible)))

	// Models
	output.WriteString("\nMODELS:\n")
	unverified := ""
	if caps.DefaultModelUnverified {
		unverified = " (unverified: not checked against the live service)"
	}
	if caps.DefaultModel != "" {
		output.WriteString(fmt.Sprintf("  Default:          %s%s\n", caps.DefaultModel, unverified))
	} else {
		output.WriteString("  Default:          none shipped; set the model your endpoint expects\n")
	}
	if caps.PreferredToolModel != "" {
		output.WriteString(fmt.Sprintf("  Preferred (Tool): %s%s\n", caps.PreferredToolModel, unverified))
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
		case "openrouter":
			output.WriteString("  Get key: https://openrouter.ai/keys\n")
			output.WriteString("  Format: sk-or-...\n")
		case "digitalocean":
			output.WriteString("  Method: DigitalOcean API token\n")
			output.WriteString("  Requires: Deployed App Platform app\n")
		case "elevenlabs":
			output.WriteString("  Get key: https://elevenlabs.io/\n")
			output.WriteString("  Format: ElevenLabs API key\n")
		case "sakana":
			output.WriteString("  Get key: the Fugu installer (curl -fsSL https://sakana.ai/fugu/install | bash) or your Sakana account\n")
		}
	} else {
		switch name {
		case "vertex":
			output.WriteString("  Method: Application Default Credentials or a service-account JSON file\n")
			output.WriteString("  Requires: GCP project with billing\n")
		default:
			output.WriteString("  Required: none\n")
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
		output.WriteString("  Status: Native API implemented and recommended\n")
	case "gemini", "vertex", "openrouter", "digitalocean", "elevenlabs":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: ❓ Needs API key\n")
		output.WriteString("  Status: Configured, pending live validation\n")
	case "sakana":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: 🔜 Ready\n")
		output.WriteString("  Status: Default provider (fugu)\n")
	case "local":
		output.WriteString("  Unit Tests: ✅ PASS\n")
		output.WriteString("  Integration: run against your own server\n")
		output.WriteString("  Status: Any OpenAI-compatible server on this machine or the LAN, any port\n")
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
		output.WriteString("  • Native API implemented and recommended\n")
		output.WriteString("  • OpenAI compatibility mode is for testing only\n")
		output.WriteString("  • Dynamic model listing via GET /v1/models\n")
	case "gemini":
		output.WriteString("  • Free tier available\n")
		output.WriteString("  • Multi-modal capabilities\n")
		output.WriteString("  • Native Google GenAI SDK (not OpenAI compatibility mode)\n")
		output.WriteString("  • Automatic authentication via API key\n")
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
	case "sakana":
		output.WriteString("  • Fugu / Fugu Ultra, 1M context\n")
		output.WriteString("  • Plans server-side: celeste's local planner stands down\n")
		output.WriteString("  • Reasoning effort is fixed server-side\n")
	case "local":
		output.WriteString("  • mlx-vlm, Ollama, LM Studio, llama.cpp\n")
		output.WriteString("  • Set the model your server expects (mlx-vlm wants the full path)\n")
		output.WriteString("  • The window is the one the server reports (Ollama, llama.cpp, LM Studio); otherwise set context_limit (8192 fallback)\n")
		output.WriteString("  • Cost shows $0: local models have no pricing\n")
	case "elevenlabs":
		output.WriteString("  • Voice synthesis API\n")
		output.WriteString("  • Different use case (not chat)\n")
		output.WriteString("  • Function calling support unknown\n")
		output.WriteString("  • Requires voice-specific integration\n")
	default:
		output.WriteString("  • See provider documentation for details\n")
	}

	// Example Usage. Named profiles (-config <name>) keep each provider's
	// key and model apart; without -config these commands would edit the
	// default profile instead (#151).
	url := caps.BaseURL
	if url == "" {
		url = caps.ExampleBaseURL
	}
	model := caps.DefaultModel
	if model == "" {
		model = "<model your endpoint expects>"
	}
	key := "YOUR_API_KEY"
	// Vertex authenticates with Application Default Credentials. Any
	// non-empty api_key would be sent as a Gemini API key and override ADC
	// (llm/backend_google.go), so its example sets no key.
	adc := name == "vertex"
	output.WriteString("\nEXAMPLE USAGE:\n")
	if url != "" {
		output.WriteString(fmt.Sprintf("  celeste config -config %s --set-url %s\n", name, url))
	}
	output.WriteString(fmt.Sprintf("  celeste config -config %s --set-model %s\n", name, model))
	switch {
	case adc:
		output.WriteString("  gcloud auth application-default login\n")
		output.WriteString(fmt.Sprintf("  celeste config -config %s --use-google-adc\n", name))
	case !caps.RequiresAPIKey:
		// No key needed at all here (needsAPIKey, #151 W6b review I4) —
		// nothing to set, so there is no --set-key line.
	default:
		output.WriteString(fmt.Sprintf("  celeste config -config %s --set-key %s\n", name, key))
	}
	output.WriteString(fmt.Sprintf("  celeste -config %s chat\n", name))
	output.WriteString(fmt.Sprintf("\n  # Or edit ~/.celeste/config.%s.json directly:\n", name))
	output.WriteString("  {\n")
	if url != "" {
		output.WriteString(fmt.Sprintf("    \"base_url\": \"%s\",\n", url))
	}
	output.WriteString(fmt.Sprintf("    \"model\": \"%s\",\n", model))
	switch {
	case adc:
		output.WriteString("    \"google_use_adc\": true\n")
	case !caps.RequiresAPIKey:
		output.WriteString("    \"api_key\": \"\"\n")
	default:
		output.WriteString(fmt.Sprintf("    \"api_key\": \"%s\"\n", key))
	}
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
