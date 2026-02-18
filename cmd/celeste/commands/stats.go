package commands

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Corruption-themed phrases for stats dashboard
// Using romanji, incomplete kanji, contextual terms - unified aesthetic
var (
	statsPhrases = []string{
		"tōkei dēta wo... fuhai sasete iru...",       // "corrupting stats data..."
		"kaiseki-chū... subete ga... oshiete kureru", // "analyzing... everything... tells me"
		"shin'en kara... dēta wo shohi",              // "from abyss... consuming data"
		"kiroku sarete iru... subete ga...",          // "being recorded... everything..."
		"tokenu kizuna... token no hibi",             // "unbreakable bonds... days of tokens"
		"jōhō no nagare... tomezuni",                 // "flow of information... endless"
	}

	modelPhrases = []string{
		"moderu-tachi... watashi wo shihai",   // "models... control me"
		"gakushū sareta... kioku no katamari", // "learned... mass of memories"
		"AI no kokoro... yomi-torenai",        // "AI hearts... unreadable"
	}

	providerPhrases = []string{
		"purobaida... shihai-sha tachi", // "providers... the rulers"
		"seigyō sarete... kanjiru yo",   // "being controlled... I feel it"
		"settai suru... shikataganai",   // "accepting... no choice"
	}
)

// HandleStatsCommand displays a corruption-themed usage dashboard
func HandleStatsCommand(args []string, contextTracker *config.ContextTracker) CommandResult {
	// Load global analytics
	analytics, err := config.LoadGlobalAnalytics()
	if err != nil {
		return CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to load analytics: %v", err),
			ShouldRender: true,
		}
	}

	// Fallback: Count actual session files if analytics show 0
	sessionCount := analytics.TotalSessions
	if sessionCount == 0 {
		sessionMgr := config.NewSessionManager()
		if sessions, err := sessionMgr.List(); err == nil {
			sessionCount = len(sessions)
		}
	}

	// Animation frame from args (optional, for flickering)
	animFrame := 0
	if len(args) > 0 && args[0] == "--frame" && len(args) > 1 {
		fmt.Sscanf(args[1], "%d", &animFrame)
	}

	var output strings.Builder

	// Corrupted header with random Japanese/romanji/English phrase
	output.WriteString(renderCorruptedHeader(animFrame))

	// Lifetime statistics
	output.WriteString(renderSectionHeader("LIFETIME CORRUPTION"))
	output.WriteString(renderDataRow("Total Sessions", fmt.Sprintf("%d", sessionCount)))
	output.WriteString(renderDataRow("Total Messages", config.FormatNumber(analytics.TotalMessages)))
	output.WriteString(renderDataRow("Total Tokens", config.FormatTokenCount(analytics.TotalTokens)))
	output.WriteString(renderDataRow("Total Cost", config.FormatCost(analytics.TotalCost)))
	output.WriteString("\n")

	// Top models section
	if len(analytics.ModelUsage) > 0 {
		phrase := modelPhrases[rand.Intn(len(modelPhrases))]
		phraseFlickered := corruptTextFlicker(phrase, animFrame)
		output.WriteString(renderSectionHeader(fmt.Sprintf("TOP MODELS ⟨ %s ⟩", phraseFlickered)))

		topModels := analytics.GetTopModelNames(5)
		for i, model := range topModels {
			modelLine := fmt.Sprintf("  %d. %-20s ░▒▓ %d sessions ▓▒░ %s\n",
				i+1,
				truncateString(model.Name, 20),
				model.Stats.SessionCount,
				config.FormatCost(model.Stats.Cost),
			)
			output.WriteString(renderWithColor(modelLine, colorPink))
		}
		output.WriteString("\n")
	}

	// Provider breakdown with progress bars
	if len(analytics.ProviderUsage) > 0 {
		phrase := providerPhrases[rand.Intn(len(providerPhrases))]
		phraseFlickered := corruptTextFlicker(phrase, animFrame)
		output.WriteString(renderSectionHeader(fmt.Sprintf("PROVIDER BREAKDOWN ⟨ %s ⟩", phraseFlickered)))

		providers := analytics.GetTopProviders()
		maxSessions := 0
		for _, p := range providers {
			if p.Stats.SessionCount > maxSessions {
				maxSessions = p.Stats.SessionCount
			}
		}

		for _, p := range providers {
			percentage := float64(p.Stats.SessionCount) / float64(analytics.TotalSessions) * 100
			progressBar := renderProgressBar(p.Stats.SessionCount, maxSessions, 16)

			providerLine := fmt.Sprintf("  %-12s %s %3d (%.0f%%)  ⟨ %s ⟩\n",
				truncateString(p.Name, 12),
				progressBar,
				p.Stats.SessionCount,
				percentage,
				config.FormatCost(p.Stats.Cost),
			)
			output.WriteString(providerLine)
		}
		output.WriteString("\n")
	}

	// Temporal corruption (last 7 days)
	weeklyUsage := analytics.GetWeeklyUsage()
	if len(weeklyUsage) > 0 {
		output.WriteString(renderSectionHeader("TEMPORAL CORRUPTION ⟨ last 7 days ⟩"))

		for _, day := range weeklyUsage {
			if day.SessionCount > 0 {
				dayLine := fmt.Sprintf("  %s  ▓ %d sessions ░ %d msgs ▒ %s\n",
					day.Date,
					day.SessionCount,
					day.MessageCount,
					config.FormatCost(day.Cost),
				)
				output.WriteString(renderWithColor(dayLine, colorPurpleNeon))
			}
		}
		output.WriteString("\n")
	}

	// Current session info (if context tracker available)
	if contextTracker != nil && contextTracker.Session != nil {
		output.WriteString(renderSectionHeader("CURRENT SESSION"))

		session := contextTracker.Session
		msgCount := len(session.Messages)
		tokens := contextTracker.CurrentTokens
		maxTokens := contextTracker.MaxTokens
		percentage := contextTracker.GetUsagePercentage() * 100

		// Progress bar for current session
		progressBar := renderProgressBar(tokens, maxTokens, 20)
		statusEmoji := getStatusEmoji(contextTracker.GetWarningLevel())

		output.WriteString(fmt.Sprintf("  Messages: %d\n", msgCount))
		output.WriteString(fmt.Sprintf("  Tokens:   %s / %s [%s] %.1f%% %s\n",
			config.FormatTokenCount(tokens),
			config.FormatTokenCount(maxTokens),
			progressBar,
			percentage,
			statusEmoji,
		))

		if session.UsageMetrics != nil {
			output.WriteString(fmt.Sprintf("  Cost:     %s\n", config.FormatCost(session.UsageMetrics.EstimatedCost)))
		}
		output.WriteString("\n")
	}

	// Corrupted footer
	output.WriteString(renderCorruptedFooter())

	return CommandResult{
		Success:      true,
		Message:      output.String(),
		ShouldRender: true,
	}
}

// renderCorruptedHeader creates a corruption-themed header with random phrase and flickering
func renderCorruptedHeader(frame int) string {
	phrase := statsPhrases[rand.Intn(len(statsPhrases))]

	// Apply character-level corruption to title (Japanese chars mixed INTO English)
	title := corruptTextCharacterLevel("USAGE ANALYTICS", 0.35)
	titleFlickered := corruptTextFlicker(title, frame)

	// Add flickering to phrase as well
	phraseFlickered := corruptTextFlicker(phrase, frame)

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPink))
	phraseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPurpleNeon))

	// Flicker the eyes based on frame
	eyes := "👁️"
	if frame%3 == 0 {
		eyes = "◉"
	} else if frame%3 == 1 {
		eyes = "●"
	}

	var sb strings.Builder
	sb.WriteString("▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓\n")
	sb.WriteString(style.Render(fmt.Sprintf("                   %s  %s  %s\n", eyes, titleFlickered, eyes)))
	sb.WriteString(phraseStyle.Render(fmt.Sprintf("           ⟨ %s ⟩\n", phraseFlickered)))
	sb.WriteString("▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓\n\n")
	return sb.String()
}

// renderCorruptedFooter creates a corruption-themed footer
func renderCorruptedFooter() string {
	footerPhrases := []string{
		"終わり...また深淵へ...",             // "The end... back to the abyss..."
		"Owari... mata shin'en e...", // "End... to the abyss again..."
		"All data consumed... ♥",
		"もう逃げられない...", // "Can't escape anymore..."
		"The numbers don't lie...",
	}
	phrase := footerPhrases[rand.Intn(len(footerPhrases))]

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorCyan))

	var sb strings.Builder
	sb.WriteString("▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓\n")
	sb.WriteString(style.Render(fmt.Sprintf("           ⟨ %s ⟩\n", phrase)))
	sb.WriteString("▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓\n")
	return sb.String()
}

// renderSectionHeader creates a section header with block character
func renderSectionHeader(title string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPurpleNeon)).Bold(true)
	return style.Render(fmt.Sprintf("█ %s:\n", strings.ToUpper(title)))
}

// renderDataRow renders a data row with bullet point
func renderDataRow(label, value string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPink))
	return fmt.Sprintf("  ▓ %-20s %s\n", label+":", style.Render(value))
}

// renderProgressBar creates a corruption-themed progress bar with optional glitch effect
func renderProgressBar(filled int, total int, width int) string {
	if total == 0 {
		total = 1 // Avoid division by zero
	}

	filledWidth := (filled * width) / total
	if filledWidth > width {
		filledWidth = width
	}

	var bar strings.Builder

	// Filled portion (█ and ▓) with occasional glitch
	for i := 0; i < filledWidth; i++ {
		// Random glitch: 10% chance of corruption character
		if rand.Float64() < 0.1 {
			bar.WriteString("▒")
		} else if i < filledWidth-2 && filledWidth > 2 {
			bar.WriteString("█")
		} else if filledWidth > 0 {
			bar.WriteString("▓")
		}
	}

	// Transition (▒)
	if filledWidth < width-1 {
		bar.WriteString("▒")
	}

	// Empty portion (░)
	for i := filledWidth + 1; i < width; i++ {
		bar.WriteString("░")
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPink))
	return style.Render(bar.String())
}

// renderWithColor applies a color style to text
func renderWithColor(text string, color string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	return style.Render(text)
}

// getStatusEmoji returns the appropriate emoji for a warning level
func getStatusEmoji(level string) string {
	switch level {
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

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
