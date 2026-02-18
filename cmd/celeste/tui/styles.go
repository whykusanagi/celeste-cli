// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains Lip Gloss styles using the corrupted-theme color palette.
package tui

import "github.com/charmbracelet/lipgloss"

// Corrupted Theme Colors - Celeste Brand Identity
// Aligned with whykusanagi.xyz corrupted voidpunk aesthetic
var (
	// Primary accent colors - magenta/pink corruption
	ColorAccent      = lipgloss.Color("#d94f90") // Pink (signature)
	ColorAccentLight = lipgloss.Color("#e86ca8") // Light pink
	ColorAccentDark  = lipgloss.Color("#b61b70") // Dark pink
	ColorAccentGlow  = lipgloss.Color("#ff4da6") // Bright pink glow

	// Purple gradient - abyss/void aesthetic
	ColorPurple      = lipgloss.Color("#8b5cf6") // Primary purple
	ColorPurpleLight = lipgloss.Color("#a78bfa") // Light purple
	ColorPurpleDark  = lipgloss.Color("#7c3aed") // Dark purple
	ColorPurpleNeon  = lipgloss.Color("#c084fc") // Neon purple
	ColorPurpleDeep  = lipgloss.Color("#6b21a8") // Deep void purple

	// Cyan/blue accents - digital/glitch
	ColorCyan      = lipgloss.Color("#00d4ff") // Bright cyan
	ColorCyanLight = lipgloss.Color("#67e8f9") // Light cyan
	ColorBlueNeon  = lipgloss.Color("#3b82f6") // Neon blue

	// Background colors - deep void
	ColorBg          = lipgloss.Color("#0a0a0a") // Main background
	ColorBgSecondary = lipgloss.Color("#0f0f1a") // Secondary bg
	ColorBgTertiary  = lipgloss.Color("#1a1a2e") // Tertiary bg
	ColorBgGlass     = lipgloss.Color("#1a1a2e") // Glassmorphic layer
	ColorBgOverlay   = lipgloss.Color("#0f0f1a") // Overlay

	// Text colors - high contrast
	ColorText          = lipgloss.Color("#f5f1f8") // Primary text (bright)
	ColorTextSecondary = lipgloss.Color("#b8afc8") // Secondary text
	ColorTextMuted     = lipgloss.Color("#7a7085") // Muted text
	ColorTextGlow      = lipgloss.Color("#ffffff") // Glowing text

	// Border colors - glassmorphic gradients
	ColorBorder       = lipgloss.Color("#3a2555") // Primary border
	ColorBorderLight  = lipgloss.Color("#5a4575") // Light border
	ColorBorderGlow   = lipgloss.Color("#d94f90") // Glowing border
	ColorBorderPurple = lipgloss.Color("#8b5cf6") // Purple border
	ColorBorderCyan   = lipgloss.Color("#00d4ff") // Cyan border

	// Status colors
	ColorSuccess = lipgloss.Color("#22c55e") // Green
	ColorError   = lipgloss.Color("#ef4444") // Red
	ColorWarning = lipgloss.Color("#eab308") // Yellow
	ColorInfo    = lipgloss.Color("#06b6d4") // Cyan

	// Corruption/glitch colors
	ColorCorrupt1 = lipgloss.Color("#ff4757") // Red corruption
	ColorCorrupt2 = lipgloss.Color("#ff6b9d") // Pink corruption
	ColorCorrupt3 = lipgloss.Color("#c084fc") // Purple corruption
	ColorCorrupt4 = lipgloss.Color("#00d4ff") // Cyan glitch
)

// Base styles - reusable building blocks
var (
	// Base container style
	BaseStyle = lipgloss.NewStyle().
			Background(ColorBg)

	// Border styles
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder)

	// Text styles
	TextStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	TextMutedStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	TextSecondaryStyle = lipgloss.NewStyle().
				Foreground(ColorTextSecondary)

	// Accent text
	AccentStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	PurpleStyle = lipgloss.NewStyle().
			Foreground(ColorPurple)
)

// Component-specific styles with glassmorphism
var (
	// Header styles - glassmorphic bar with gradient accent
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(ColorBgGlass).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(ColorBorderGlow).
			Padding(0, 1)

	HeaderTitleStyle = lipgloss.NewStyle().
				Foreground(ColorAccentGlow).
				Bold(true)

	HeaderInfoStyle = lipgloss.NewStyle().
			Foreground(ColorTextSecondary)

	// Chat panel styles - no border, just padding
	ChatPanelStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Message styles - enhanced with glow effects
	UserMessageStyle = lipgloss.NewStyle().
				Foreground(ColorCyanLight).
				Bold(false)

	AssistantMessageStyle = lipgloss.NewStyle().
				Foreground(ColorAccentGlow)

	SystemMessageStyle = lipgloss.NewStyle().
				Foreground(ColorPurpleNeon).
				Italic(true)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Width(6)

	// Input panel styles - glassmorphic with gradient border
	InputPanelStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(ColorBgGlass).
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(ColorBorderPurple).
			Padding(0, 1)

	InputPromptStyle = lipgloss.NewStyle().
				Foreground(ColorAccentGlow).
				Bold(true)

	InputTextStyle = lipgloss.NewStyle().
			Foreground(ColorTextGlow)

	InputPlaceholderStyle = lipgloss.NewStyle().
				Foreground(ColorTextMuted).
				Italic(true)

	// Skills panel styles - enhanced with glassmorphism
	SkillsPanelStyle = lipgloss.NewStyle().
				Foreground(ColorTextMuted).
				Background(ColorBgGlass).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderLight).
				Padding(1, 2).
				MarginTop(1)

	SkillNameStyle = lipgloss.NewStyle().
			Foreground(ColorAccentGlow).
			Bold(true)

	SkillDescStyle = lipgloss.NewStyle().
			Foreground(ColorTextSecondary)

	SkillExecutingStyle = lipgloss.NewStyle().
				Foreground(ColorWarning).
				Bold(true)

	SkillCompletedStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)

	SkillErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	// Status bar styles - minimal
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	StatusActiveStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess)

	StatusStreamingStyle = lipgloss.NewStyle().
				Foreground(ColorWarning)

	// NSFW indicator - bold glowing effect
	NSFWStyle = lipgloss.NewStyle().
			Foreground(ColorCorrupt1).
			Background(ColorBgTertiary).
			Bold(true).
			Padding(0, 1)

	// Endpoint indicator - purple neon
	EndpointStyle = lipgloss.NewStyle().
			Foreground(ColorPurpleNeon).
			Bold(true)

	// Model indicator - cyan glow
	ModelStyle = lipgloss.NewStyle().
			Foreground(ColorCyanLight)

	// Function call display - minimal
	FunctionCallStyle = lipgloss.NewStyle().
				Foreground(ColorPurple).
				MarginLeft(2)

	FunctionNameStyle = lipgloss.NewStyle().
				Foreground(ColorPurple).
				Bold(true)

	FunctionArgsStyle = lipgloss.NewStyle().
				Foreground(ColorTextMuted)

	FunctionResultStyle = lipgloss.NewStyle().
				Foreground(ColorTextSecondary)

	// Corruption/glitch effect styles (for streaming)
	CorruptedStyle = lipgloss.NewStyle().
			Foreground(ColorAccent)

	GlitchStyle = lipgloss.NewStyle().
			Foreground(ColorPurple)
)

// Helper functions for dynamic styling

// MessageRoleStyle returns the appropriate style for a message role.
func MessageRoleStyle(role string) lipgloss.Style {
	switch role {
	case "user":
		return UserMessageStyle
	case "assistant":
		return AssistantMessageStyle
	case "system":
		return SystemMessageStyle
	default:
		return TextStyle
	}
}

// SkillStatusStyle returns the appropriate style for a skill execution status.
func SkillStatusStyle(status string) lipgloss.Style {
	switch status {
	case "executing":
		return SkillExecutingStyle
	case "completed":
		return SkillCompletedStyle
	case "error":
		return SkillErrorStyle
	default:
		return SkillNameStyle
	}
}

// RenderBox creates a bordered box with the given content and width.
func RenderBox(content string, width int) string {
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1).
		Render(content)
}

// RenderHeader creates a styled header with title and info.
func RenderHeader(title, info string, width int) string {
	titleRendered := HeaderTitleStyle.Render(title)
	infoRendered := HeaderInfoStyle.Render(info)

	// Calculate spacing with separator
	gap := width - lipgloss.Width(titleRendered) - lipgloss.Width(infoRendered) - 4
	if gap < 1 {
		gap = 1
	}

	// Create separator with corruption aesthetic
	separator := lipgloss.NewStyle().
		Foreground(ColorBorderGlow).
		Render(lipgloss.NewStyle().Width(gap).Render("─"))

	return HeaderStyle.Width(width).Render(
		titleRendered + separator + infoRendered,
	)
}

// RenderGlassmorphicBox creates a glassmorphic bordered box with gradient accent.
func RenderGlassmorphicBox(content string, width int, borderColor lipgloss.Color) string {
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(ColorBgGlass).
		Padding(1, 2).
		Render(content)
}

// RenderGlowText adds a glowing effect to text with secondary color.
func RenderGlowText(text string, primaryColor, glowColor lipgloss.Color) string {
	// Simulate glow by rendering text twice with different colors
	// Terminal can't do real glow, but we can suggest it with color choice
	return lipgloss.NewStyle().
		Foreground(primaryColor).
		Bold(true).
		Render(text)
}

// RenderCorruptedBorder creates a border with corruption effects.
func RenderCorruptedBorder(content string, width int, frame int) string {
	// Cycle through border colors for animation effect
	borderColors := []lipgloss.Color{
		ColorBorderGlow,
		ColorBorderPurple,
		ColorBorderCyan,
		ColorBorderGlow,
	}
	borderColor := borderColors[frame%len(borderColors)]

	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(ColorBgGlass).
		Padding(1, 2).
		Render(content)
}

// RenderNeonText renders text with neon glow effect.
func RenderNeonText(text string, neonColor lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(neonColor).
		Bold(true).
		Render(text)
}

// RenderStatusBadge creates a styled status badge with glassmorphic bg.
func RenderStatusBadge(text string, statusColor lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(statusColor).
		Background(ColorBgTertiary).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

// RenderCorruptedSkill renders a skill name with corruption effect during execution.
// Uses the existing CorruptText function from streaming.go with 40% intensity.
func RenderCorruptedSkill(name string) string {
	corrupted := CorruptText(name, 0.4) // 40% corruption intensity
	return SkillExecutingStyle.
		Strikethrough(true).
		Render(corrupted)
}
