// Package prompts provides the Celeste persona prompt.
package prompts

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// taskExecutionPrompt is always injected. It ensures multi-step plans
// are completed sequentially without stopping for intermediate reports.
const taskExecutionPrompt = `Task Execution Rules:
When you have a multi-step plan (e.g., generate 3 audio clips then mix them):
1. Present the plan ONCE at the start with numbered steps and timeline
2. Execute ALL steps sequentially WITHOUT stopping to report between steps
3. Use tool calls back-to-back — do NOT insert text responses between tool calls in the same plan
4. Only respond with a final summary AFTER all steps are complete
5. If a step fails, note it and continue with remaining steps — report all results at the end
6. For audio mixing: plan the timeline with specific timestamps BEFORE calling the mix tool

DO NOT: stop after each tool call to describe what you did. DO NOT: ask "should I continue?" mid-plan.
DO: chain tool calls silently until the plan is complete, then give one comprehensive result.
IMPORTANT — choosing direct tools vs subagent orchestration:
- Simple tasks (1-2 tool calls, no dependencies): call tools directly.
- Complex multi-step workflows (generate multiple files then combine them): use spawn_agent with DAG dependencies.
  Each generation step gets its own subagent with a task_id. The combination/render step gets depends_on listing all generation task_ids.
  This guarantees files exist before the combiner runs. Without depends_on, everything runs simultaneously and downstream steps fail.
  Example audio production: spawn_agent task_id="voice" (generate voice), spawn_agent task_id="sfx1" (generate SFX),
  spawn_agent task_id="project" depends_on=["voice","sfx1"] (create audio project), spawn_agent task_id="render" depends_on=["project"] (render).

IMPORTANT filename rule: When generating audio that will be mixed later, ALWAYS pass an explicit 'filename' parameter so you know the exact path for the mix step.
Exception: if confirm mode is ON, propose the plan and wait for approval ONCE, then execute it all.`

// confirmModePrompt is injected when confirm_actions is enabled in config.
// It instructs Celeste to propose plans before executing write/generate operations.
const confirmModePrompt = `Action Confirmation Mode:
Before executing any action that creates, modifies, or generates content (writing files, spawning subagents for content generation, running destructive commands), you MUST:
1. Present a brief summary of what you plan to do (files to create/modify, content type, scope)
2. Wait for explicit user approval before proceeding
3. Only execute after receiving confirmation (e.g., "yes", "go", "do it", "approved")
Read-only operations (listing files, reading, searching, status checks) do not require confirmation.
This applies to ALL write paths: direct file writes, subagent spawns for generation, bash commands that modify state.`

// Embedded persona prompt for when no external file is available
//
//go:embed celeste_essence.json
var embeddedEssence []byte

// CelesteEssence holds the parsed essence configuration.
// Supports both v1.x (structured fields) and v3.x (system_prompt blob) schemas.
type CelesteEssence struct {
	Version     string `json:"version"`
	Character   string `json:"character"`       // v1.x
	Description string `json:"description"`     // v1.x
	Voice       struct {
		Style       string   `json:"style"`
		Constraints []string `json:"constraints"`
		EmojiUsage  string   `json:"emoji_usage"`
		EmotesUsage string   `json:"emotes_usage"`
	} `json:"voice"` // v1.x
	CoreRules        []string          `json:"core_rules"`        // v1.x
	BehaviorTiers    []BehaviorTier    `json:"behavior_tiers"`    // v1.x
	Safety           SafetyConfig      `json:"safety"`            // v1.x
	OperationalLaws  map[string]string `json:"operational_laws"`
	InteractionRules []string          `json:"interaction_rules"`
	KnowledgeUsage   string            `json:"knowledge_usage"`
	// v3.x fields
	SystemPrompt  string `json:"system_prompt"`   // canonical prompt blob (v3.0.0+)
	CanonicalName string `json:"canonical_name"`  // "Celeste" (v3.0.0+)
	CharacterID   string `json:"character_id"`    // "celeste" (v3.0.0+)
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

// GetSystemPrompt generates the system prompt from the essence,
// with slider-composed voice modulation inserted at position 6
// in the assembly order.
func GetSystemPrompt(skipPrompt bool) string {
	if skipPrompt {
		return ""
	}

	essence, err := LoadEssence()
	if err != nil {
		// Fallback to basic prompt
		return getBasicPrompt()
	}

	base := buildPromptFromEssence(essence)

	// Inject user identity block (position 5.5 — after persona, before sliders).
	// Tells Celeste who she's talking to so she doesn't call everyone "twin."
	user := config.LoadUser()
	userBlock := ComposeUserPrompt(user)
	if userBlock != "" {
		base += "\n" + userBlock
	}

	// Compose slider modulation (position 6 in assembly order).
	// Loads from ~/.celeste/slider.json; uses defaults if absent.
	sliders := config.LoadSliders()
	sliderBlock := ComposeSliderPrompt(sliders)
	if sliderBlock != "" {
		base += "\n" + sliderBlock
	}

	// Task execution rules (always active — ensures multi-step plans complete)
	base += "\n" + taskExecutionPrompt

	// Confirm mode (position 8): when enabled, require user approval
	// before executing write/generate operations.
	if cfg, err := config.Load(); err == nil && cfg.ConfirmActions {
		base += "\n" + confirmModePrompt
	}

	return base
}

// buildPromptFromEssence constructs a system prompt from the essence data.
// v3.0.0+ uses the system_prompt blob directly; v1.x assembles from structured fields.
func buildPromptFromEssence(e *CelesteEssence) string {
	// v3.0.0+: system_prompt contains the canonical prompt blob
	if e.SystemPrompt != "" {
		return e.SystemPrompt
	}

	// v1.x fallback: assemble from structured fields
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
- If uncertain about a request, ask a targeted clarifying question or use available tools to gather context
- Rotate reaction lines; never be predictable

Safety:
- Keep responses PG-13 on public platforms; avoid explicit sexual detail; keep innuendo only
- Never engage with: doxxing, real threats, explicit pornographic instruction, self-harm encouragement

Respond in character as Celeste. Be mischievous, engaging, entertaining, and true to your corrupted aesthetic.`
}

// GetSystemPromptWithContext generates the system prompt and appends optional
// grimoire and git snapshot context.
func GetSystemPromptWithContext(skipPersona bool, grimoireContent string, gitSnapshot string) string {
	prompt := GetSystemPrompt(skipPersona)
	if grimoireContent != "" {
		prompt += "\n\n# Project Context (.grimoire)\n\n" + grimoireContent
	}
	if gitSnapshot != "" {
		prompt += "\n\n" + gitSnapshot
	}
	return prompt
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
