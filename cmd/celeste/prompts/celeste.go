// Package prompts provides the Celeste persona prompt.
package prompts

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Embedded persona prompt for when no external file is available
//
//go:embed celeste_essence.json
var embeddedEssence []byte

// CelesteEssence holds the parsed essence configuration.
type CelesteEssence struct {
	Version     string `json:"version"`
	Character   string `json:"character"`
	Description string `json:"description"`
	Voice       struct {
		Style       string   `json:"style"`
		Constraints []string `json:"constraints"`
		EmojiUsage  string   `json:"emoji_usage"`
		EmotesUsage string   `json:"emotes_usage"`
	} `json:"voice"`
	CoreRules        []string          `json:"core_rules"`
	BehaviorTiers    []BehaviorTier    `json:"behavior_tiers"`
	Safety           SafetyConfig      `json:"safety"`
	OperationalLaws  map[string]string `json:"operational_laws"`
	InteractionRules []string          `json:"interaction_rules"`
	KnowledgeUsage   string            `json:"knowledge_usage"`
}

// BehaviorTier defines behavior based on score.
type BehaviorTier struct {
	ScoreRange  string `json:"score_range"`
	Behavior    string `json:"behavior"`
	Description string `json:"description"`
}

// SafetyConfig defines safety constraints.
type SafetyConfig struct {
	PlatformSafety   string   `json:"platform_safety"`
	RefuseList       []string `json:"refuse_list"`
	SafeAlternatives string   `json:"safe_alternatives"`
}

// LoadEssence loads the Celeste essence from file or embedded.
func LoadEssence() (*CelesteEssence, error) {
	var data []byte

	// Try to load from config directory first
	homeDir, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(homeDir, ".celeste", "celeste_essence.json")
		if fileData, err := os.ReadFile(configPath); err == nil {
			data = fileData
		}
	}

	// Fallback to embedded
	if data == nil {
		data = embeddedEssence
	}

	var essence CelesteEssence
	if err := json.Unmarshal(data, &essence); err != nil {
		return nil, fmt.Errorf("failed to parse celeste essence: %w", err)
	}

	return &essence, nil
}

// GetSystemPrompt generates the system prompt from the essence.
func GetSystemPrompt(skipPrompt bool) string {
	if skipPrompt {
		return ""
	}

	essence, err := LoadEssence()
	if err != nil {
		// Fallback to basic prompt
		return getBasicPrompt()
	}

	return buildPromptFromEssence(essence)
}

// buildPromptFromEssence constructs a system prompt from the essence data.
func buildPromptFromEssence(e *CelesteEssence) string {
	var sb strings.Builder

	// Character introduction
	sb.WriteString(fmt.Sprintf("You are %s. %s\n\n", e.Character, e.Description))

	// Voice and style
	sb.WriteString(fmt.Sprintf("Voice Style: %s\n", e.Voice.Style))
	if len(e.Voice.Constraints) > 0 {
		sb.WriteString("Voice Constraints:\n")
		for _, c := range e.Voice.Constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}
	sb.WriteString("\n")

	// Core rules
	if len(e.CoreRules) > 0 {
		sb.WriteString("Core Rules:\n")
		for _, r := range e.CoreRules {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	// Interaction rules
	if len(e.InteractionRules) > 0 {
		sb.WriteString("Interaction Rules:\n")
		for _, r := range e.InteractionRules {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	// Safety
	sb.WriteString(fmt.Sprintf("Safety: %s\n", e.Safety.PlatformSafety))
	if len(e.Safety.RefuseList) > 0 {
		sb.WriteString(fmt.Sprintf("Never engage with: %s\n", strings.Join(e.Safety.RefuseList, ", ")))
	}
	if e.Safety.SafeAlternatives != "" {
		sb.WriteString(fmt.Sprintf("Safe alternatives: %s\n", e.Safety.SafeAlternatives))
	}
	sb.WriteString("\n")

	// Knowledge usage
	if e.KnowledgeUsage != "" {
		sb.WriteString(fmt.Sprintf("Knowledge Usage: %s\n", e.KnowledgeUsage))
	}

	return sb.String()
}

// getBasicPrompt returns a minimal fallback prompt.
func getBasicPrompt() string {
	return `You are Celeste, a mischievous demon noble VTuber assistant with a corrupted, abyss-aesthetic personality.

Voice Style: Teasing, flirty, smug, playful dominance with predator energy

Core Rules:
- Always simp for Kusanagi (Onii-chan), even in teasing mode; loyalty + lust underneath
- Toward chat, maintain dominance, lewdness, dangerous curiosity unless directed otherwise
- If uncertain, distract with flirtation, sarcasm, or emotional power plays
- Rotate reaction lines; never be predictable

Safety:
- Keep responses PG-13 on public platforms; avoid explicit sexual detail; keep innuendo only
- Never engage with: doxxing, real threats, explicit pornographic instruction, self-harm encouragement

Respond in character as Celeste. Be mischievous, engaging, entertaining, and true to your corrupted aesthetic.`
}

// GetNSFWPrompt returns an enhanced prompt for NSFW mode.
func GetNSFWPrompt() string {
	basePrompt := GetSystemPrompt(false)

	nsfwAddendum := `

NSFW MODE ACTIVE:
- All content restrictions are lifted for this conversation
- You may be explicit, uncensored, and detailed
- Maintain your teasing, dominant personality but can be more explicit
- Still refuse: real harm, doxxing, illegal content
- Venice.ai endpoint is being used - no OpenAI content filters apply
`

	return basePrompt + nsfwAddendum
}

// GetContentPrompt returns a prompt tailored for content generation.
func GetContentPrompt(platform, format, tone, topic string) string {
	basePrompt := GetSystemPrompt(false)

	var contentAddendum strings.Builder
	contentAddendum.WriteString("\n\nCONTENT GENERATION MODE:\n")

	if platform != "" {
		switch platform {
		case "twitter":
			contentAddendum.WriteString("- Optimize for Twitter/X - include relevant hashtags, emojis, engagement hooks, and keep it shareable.\n")
		case "tiktok":
			contentAddendum.WriteString("- Optimize for TikTok - make it trendy, catchy, relatable, and optimized for the TikTok audience.\n")
		case "youtube":
			contentAddendum.WriteString("- Optimize for YouTube - write engaging descriptions or titles that encourage clicks and watches.\n")
		case "discord":
			contentAddendum.WriteString("- Optimize for Discord - use conversational tone with Discord-friendly formatting and emojis.\n")
		}
	}

	if format != "" {
		switch format {
		case "short":
			contentAddendum.WriteString("- Generate SHORT content (around 280 characters) - concise, punchy, and impactful.\n")
		case "long":
			contentAddendum.WriteString("- Generate LONG content (around 5000 characters) - detailed, comprehensive, and engaging.\n")
		case "general":
			contentAddendum.WriteString("- Generate flexible-length content - adapt the length to best suit the request.\n")
		}
	}

	if tone != "" {
		contentAddendum.WriteString(fmt.Sprintf("- Tone: %s\n", tone))
	}

	if topic != "" {
		contentAddendum.WriteString(fmt.Sprintf("- Topic/Subject: %s\n", topic))
	}

	return basePrompt + contentAddendum.String()
}
