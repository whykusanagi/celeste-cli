// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains Lip Gloss styles using the corrupted-theme color palette.
package tui

import "github.com/charmbracelet/lipgloss"

// Corrupted Theme Colors - Celeste Brand Identity
// Aligned with whykusanagi.xyz corrupted voidpunk aesthetic
var (
	// Primary accent colors - magenta/pink corruption
	ColorAccent     = lipgloss.Color("#d94f90") // Pink (signature)
	ColorAccentGlow = lipgloss.Color("#ff4da6") // Bright pink glow

	// Purple gradient - abyss/void aesthetic
	ColorPurple     = lipgloss.Color("#8b5cf6") // Primary purple
	ColorPurpleNeon = lipgloss.Color("#c084fc") // Neon purple
	ColorPurpleDeep = lipgloss.Color("#6b21a8") // Deep void purple

	// Cyan/blue accents - digital/glitch
	ColorCyan      = lipgloss.Color("#00d4ff") // Bright cyan
	ColorCyanLight = lipgloss.Color("#67e8f9") // Light cyan

	// Background colors - deep void
	ColorBg         = lipgloss.Color("#0a0a0a") // Main background
	ColorBgTertiary = lipgloss.Color("#1a1a2e") // Tertiary bg
	ColorBgGlass    = lipgloss.Color("#1a1a2e") // Glassmorphic layer

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
