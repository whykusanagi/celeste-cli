package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ContextIndicator displays token usage with visual progress bar and color coding
type ContextIndicator struct {
	width           int
	currentTokens   int
	maxTokens       int
	warningLevel    string // "ok", "warn", "caution", "critical"
	showPercentage  bool
	showProgressBar bool
}

// NewContextIndicator creates a new context indicator
func NewContextIndicator() ContextIndicator {
	return ContextIndicator{
		width:           30,
		showPercentage:  true,
		showProgressBar: true,
		warningLevel:    "ok",
	}
}

// SetWidth sets the indicator width
func (ci ContextIndicator) SetWidth(width int) ContextIndicator {
	ci.width = width
	return ci
}

// SetUsage updates the current token usage
func (ci ContextIndicator) SetUsage(current, max int) ContextIndicator {
	ci.currentTokens = current
	ci.maxTokens = max
	ci.warningLevel = ci.calculateWarningLevel()
	return ci
}

// SetWarningLevel explicitly sets the warning level
func (ci ContextIndicator) SetWarningLevel(level string) ContextIndicator {
	ci.warningLevel = level
	return ci
}

// SetShowPercentage controls percentage display
func (ci ContextIndicator) SetShowPercentage(show bool) ContextIndicator {
	ci.showPercentage = show
	return ci
}

// SetShowProgressBar controls progress bar display
func (ci ContextIndicator) SetShowProgressBar(show bool) ContextIndicator {
	ci.showProgressBar = show
	return ci
}

// View renders the context indicator
func (ci ContextIndicator) View() string {
	if ci.maxTokens == 0 {
		return ""
	}

	parts := []string{}

	// Token count display with color
	tokenText := ci.formatTokenDisplay()
	parts = append(parts, ci.getColorStyle().Render(tokenText))

	// Progress bar (optional)
	if ci.showProgressBar && ci.width > 20 {
		barWidth := ci.width - len(tokenText) - 4 // Leave space for padding
		if barWidth > 10 {
			progressBar := ci.renderProgressBar(barWidth)
			parts = append(parts, progressBar)
		}
	}

	// Percentage (optional)
	if ci.showPercentage {
		percentage := ci.getUsagePercentage()
		percentText := fmt.Sprintf("%.1f%%", percentage*100)
		parts = append(parts, ci.getColorStyle().Render(percentText))
	}

	return strings.Join(parts, " ")
}

// ViewCompact renders a compact version (for header use)
func (ci ContextIndicator) ViewCompact() string {
	if ci.maxTokens == 0 {
		return ""
	}

	tokenText := ci.formatTokenDisplay()
	percentage := ci.getUsagePercentage()
	percentText := fmt.Sprintf("(%.1f%%)", percentage*100)

	emoji := ci.getStatusEmoji()
	combined := fmt.Sprintf("%s %s %s", emoji, tokenText, percentText)

	return ci.getColorStyle().Render(combined)
}

// formatTokenDisplay formats the token count as "X.XK / Y.YK"
func (ci ContextIndicator) formatTokenDisplay() string {
	current := ci.formatTokenCount(ci.currentTokens)
	max := ci.formatTokenCount(ci.maxTokens)
	return fmt.Sprintf("%s/%s", current, max)
}

// formatTokenCount formats a token count with K/M suffix
func (ci ContextIndicator) formatTokenCount(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	} else if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// renderProgressBar creates a visual progress bar
func (ci ContextIndicator) renderProgressBar(width int) string {
	if width < 3 {
		return ""
	}

	percentage := ci.getUsagePercentage()
	filled := int(float64(width) * percentage)
	if filled > width {
		filled = width
	}

	var bar strings.Builder
	bar.WriteString("[")

	// Filled portion
	for i := 0; i < filled; i++ {
		bar.WriteString("█")
	}

	// Empty portion
	for i := filled; i < width; i++ {
		bar.WriteString("░")
	}

	bar.WriteString("]")

	return ci.getColorStyle().Render(bar.String())
}

// getUsagePercentage returns usage as 0.0 to 1.0
func (ci ContextIndicator) getUsagePercentage() float64 {
	if ci.maxTokens == 0 {
		return 0.0
	}
	return float64(ci.currentTokens) / float64(ci.maxTokens)
}

// calculateWarningLevel determines the warning level based on usage
func (ci ContextIndicator) calculateWarningLevel() string {
	usage := ci.getUsagePercentage()

	if usage >= 0.95 {
		return "critical"
	} else if usage >= 0.85 {
		return "caution"
	} else if usage >= 0.75 {
		return "warn"
	}
	return "ok"
}

// getStatusEmoji returns an emoji for the current warning level
func (ci ContextIndicator) getStatusEmoji() string {
	switch ci.warningLevel {
	case "critical":
		return "🔴"
	case "caution":
		return "🟠"
	case "warn":
		return "🟡"
	default:
		return "🟢"
	}
}

// getColorStyle returns the appropriate color style for current warning level
func (ci ContextIndicator) getColorStyle() lipgloss.Style {
	switch ci.warningLevel {
	case "critical":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Bright red
	case "caution":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // Orange
	case "warn":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("226")) // Yellow
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // Green
	}
}

// GetWarningMessage returns a user-friendly warning message
func (ci ContextIndicator) GetWarningMessage() string {
	percentage := int(ci.getUsagePercentage() * 100)

	switch ci.warningLevel {
	case "critical":
		return fmt.Sprintf("🚨 Context at %d%% - will auto-compact on next message", percentage)
	case "caution":
		return fmt.Sprintf("⚠️  Context at %d%% - compaction recommended", percentage)
	case "warn":
		return fmt.Sprintf("⚠️  Context at %d%% - consider compaction soon", percentage)
	default:
		return ""
	}
}

// ShouldShowWarning returns true if a warning should be displayed
func (ci ContextIndicator) ShouldShowWarning() bool {
	return ci.warningLevel != "ok"
}

// GetWarningLevel returns the current warning level
func (ci ContextIndicator) GetWarningLevel() string {
	return ci.warningLevel
}

// GetUsageInfo returns formatted usage information
func (ci ContextIndicator) GetUsageInfo() string {
	percentage := ci.getUsagePercentage() * 100
	current := ci.formatTokenCount(ci.currentTokens)
	max := ci.formatTokenCount(ci.maxTokens)
	remaining := ci.formatTokenCount(ci.maxTokens - ci.currentTokens)

	return fmt.Sprintf(
		"Token Usage: %s / %s (%.1f%%) • Remaining: %s",
		current, max, percentage, remaining,
	)
}
