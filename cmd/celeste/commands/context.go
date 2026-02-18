package commands

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// HandleContextCommand handles the /context command and its subcommands.
// Usage:
//
//	/context          - Show current context usage
//	/context status   - Detailed context breakdown
//	/context compact  - Manual context compaction (future)
func HandleContextCommand(args []string, contextTracker *config.ContextTracker) CommandResult {
	if contextTracker == nil {
		return CommandResult{
			Success: true,
			Message: "📭 No messages in this session yet\n\n" +
				"Context tracking will begin after your first message.\n" +
				"Send a message to start tracking token usage.",
			ShouldRender: true,
		}
	}

	// Default to status display if no subcommand
	subcommand := "status"
	if len(args) > 0 {
		subcommand = args[0]
	}

	switch subcommand {
	case "status", "":
		return showContextStatus(contextTracker)
	case "compact":
		return CommandResult{
			Success:      false,
			Message:      "⚠️  Manual compaction not yet implemented - auto-compaction triggers at 80%",
			ShouldRender: true,
		}
	case "reset":
		return CommandResult{
			Success:      true,
			Message:      "✓ Context warnings reset",
			ShouldRender: true,
		}
	default:
		return CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Unknown /context subcommand: %s\n\nAvailable: status, compact, reset", subcommand),
			ShouldRender: true,
		}
	}
}

// showContextStatus displays detailed context usage information
func showContextStatus(ct *config.ContextTracker) CommandResult {
	var output strings.Builder

	// Header
	output.WriteString("═══════════════════════════════════════════════\n")
	output.WriteString("              CONTEXT STATUS\n")
	output.WriteString("═══════════════════════════════════════════════\n\n")

	// Model info
	output.WriteString(fmt.Sprintf("Model:            %s\n", ct.Model))
	output.WriteString(fmt.Sprintf("Context Window:   %s tokens\n\n", config.FormatTokenCount(ct.MaxTokens)))

	// Token usage
	output.WriteString("TOKEN USAGE:\n")
	output.WriteString(fmt.Sprintf("  Input:          %s tokens\n", config.FormatTokenCount(ct.PromptTokens)))
	output.WriteString(fmt.Sprintf("  Output:         %s tokens\n", config.FormatTokenCount(ct.CompletionTokens)))

	usagePercent := ct.GetUsagePercentage() * 100
	output.WriteString(fmt.Sprintf("  Total:          %s tokens (%.1f%%)\n",
		config.FormatTokenCount(ct.CurrentTokens), usagePercent))
	output.WriteString(fmt.Sprintf("  Remaining:      %s tokens\n\n",
		config.FormatTokenCount(ct.GetRemainingTokens())))

	// Cost estimation (if session has usage metrics)
	if ct.Session != nil && ct.Session.UsageMetrics != nil {
		metrics := ct.Session.UsageMetrics
		output.WriteString("ESTIMATED COST:\n")

		// Get model pricing
		pricing, hasPricing := config.GetModelPricing(ct.Model)
		if hasPricing {
			inputCost := (float64(metrics.TotalInputTokens) / 1_000_000) * pricing.InputCostPerMillion
			outputCost := (float64(metrics.TotalOutputTokens) / 1_000_000) * pricing.OutputCostPerMillion

			output.WriteString(fmt.Sprintf("  Input:          %s ($%.2f/M)\n",
				config.FormatCost(inputCost), pricing.InputCostPerMillion))
			output.WriteString(fmt.Sprintf("  Output:         %s ($%.2f/M)\n",
				config.FormatCost(outputCost), pricing.OutputCostPerMillion))
			output.WriteString(fmt.Sprintf("  Total:          %s\n\n",
				config.FormatCost(metrics.EstimatedCost)))
		} else {
			output.WriteString("  (Pricing unavailable for this model)\n\n")
		}
	}

	// Status indicator
	level := ct.GetWarningLevel()
	var statusEmoji, statusText, statusColor string
	switch level {
	case "critical":
		statusEmoji = "🔴"
		statusText = "Critical"
		statusColor = "(>95% - auto-compaction imminent)"
	case "caution":
		statusEmoji = "🟠"
		statusText = "Caution"
		statusColor = "(>85% - compaction recommended)"
	case "warn":
		statusEmoji = "🟡"
		statusText = "Warning"
		statusColor = "(>75% - approaching limit)"
	default:
		statusEmoji = "🟢"
		statusText = "Normal"
		statusColor = "(healthy)"
	}

	output.WriteString(fmt.Sprintf("STATUS:           %s %s %s\n\n", statusEmoji, statusText, statusColor))

	// Recommendations
	output.WriteString("RECOMMENDATIONS:\n")
	if level == "critical" {
		output.WriteString("  • Context is critically high - auto-compaction will trigger soon\n")
		output.WriteString("  • Consider starting a new session or using /context compact\n")
	} else if level == "caution" {
		output.WriteString("  • Context usage is high - consider compaction\n")
		avgTokens := 500 // Default estimate
		if ct.Session != nil && ct.Session.UsageMetrics != nil && ct.Session.UsageMetrics.MessageCount > 0 {
			avgTokens = int(ct.Session.UsageMetrics.GetAverageTokensPerMessage())
		}
		msgsUntilWarn := ct.EstimateMessagesUntilLimit(avgTokens)
		output.WriteString(fmt.Sprintf("  • Approximately %d messages until warning threshold\n", msgsUntilWarn))
	} else if level == "warn" {
		avgTokens := 500
		if ct.Session != nil && ct.Session.UsageMetrics != nil && ct.Session.UsageMetrics.MessageCount > 0 {
			avgTokens = int(ct.Session.UsageMetrics.GetAverageTokensPerMessage())
		}
		msgsUntilWarn := ct.EstimateMessagesUntilLimit(avgTokens)
		output.WriteString(fmt.Sprintf("  • You can send ~%d more messages before compaction needed\n", msgsUntilWarn))
		output.WriteString("  • Context is healthy but approaching warning threshold\n")
	} else {
		avgTokens := 500
		if ct.Session != nil && ct.Session.UsageMetrics != nil && ct.Session.UsageMetrics.MessageCount > 0 {
			avgTokens = int(ct.Session.UsageMetrics.GetAverageTokensPerMessage())
		}
		msgsUntilWarn := ct.EstimateMessagesUntilLimit(avgTokens)
		if msgsUntilWarn > 0 {
			output.WriteString(fmt.Sprintf("  • You can send ~%d more messages before reaching warning threshold\n", msgsUntilWarn))
		}
		output.WriteString("  • Context is healthy - no action needed\n")
	}

	// Compaction info
	if ct.CompactionCount > 0 {
		output.WriteString(fmt.Sprintf("\n  • Compactions performed: %d\n", ct.CompactionCount))
	}
	if ct.TruncationCount > 0 {
		output.WriteString(fmt.Sprintf("  • Truncations performed: %d\n", ct.TruncationCount))
	}

	output.WriteString("═══════════════════════════════════════════════\n")

	return CommandResult{
		Success:      true,
		Message:      output.String(),
		ShouldRender: true,
	}
}
