package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetContentPrompt tests content generation prompts
func TestGetContentPrompt(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		format   string
		tone     string
		topic    string
		expects  []string
	}{
		{
			name:     "Twitter short",
			platform: "twitter",
			format:   "short",
			tone:     "casual",
			topic:    "gaming",
			expects:  []string{"Twitter/X", "280 characters", "casual", "gaming"},
		},
		{
			name:     "TikTok long",
			platform: "tiktok",
			format:   "long",
			tone:     "energetic",
			topic:    "tech",
			expects:  []string{"TikTok", "5000 characters", "energetic", "tech"},
		},
		{
			name:     "YouTube general",
			platform: "youtube",
			format:   "general",
			tone:     "",
			topic:    "",
			expects:  []string{"YouTube", "flexible-length"},
		},
		{
			name:     "Discord",
			platform: "discord",
			format:   "",
			tone:     "friendly",
			topic:    "community",
			expects:  []string{"Discord", "friendly", "community"},
		},
		{
			name:     "No platform",
			platform: "",
			format:   "short",
			tone:     "professional",
			topic:    "business",
			expects:  []string{"SHORT content", "professional", "business"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := GetContentPrompt(0, tt.platform, tt.format, tt.tone, tt.topic)

			assert.NotEmpty(t, prompt, "Prompt should not be empty")
			assert.Contains(t, prompt, "CONTENT GENERATION MODE", "Should indicate content mode")

			for _, expect := range tt.expects {
				assert.Contains(t, prompt, expect, "Should contain expected string: %s", expect)
			}
		})
	}
}

// TestGetContentPromptPlatforms tests all platform options
func TestGetContentPromptPlatforms(t *testing.T) {
	platforms := []struct {
		name    string
		keyword string
	}{
		{"twitter", "Twitter/X"},
		{"tiktok", "TikTok"},
		{"youtube", "YouTube"},
		{"discord", "Discord"},
	}

	for _, p := range platforms {
		t.Run(p.name, func(t *testing.T) {
			prompt := GetContentPrompt(0, p.name, "", "", "")
			assert.Contains(t, prompt, p.keyword, "Should mention platform")
		})
	}
}

// TestGetContentPromptFormats tests all format options
func TestGetContentPromptFormats(t *testing.T) {
	formats := []struct {
		name    string
		keyword string
	}{
		{"short", "280 characters"},
		{"long", "5000 characters"},
		{"general", "flexible-length"},
	}

	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			prompt := GetContentPrompt(0, "", f.name, "", "")
			assert.Contains(t, prompt, f.keyword, "Should mention format")
		})
	}
}

// TestContentPromptIncludesBase: the content prompt is the chat prompt
// plus the content addendum.
func TestContentPromptIncludesBase(t *testing.T) {
	composeEnv(t, false)
	base := Compose(ComposeOptions{Mode: ModeChat}).String()
	contentPrompt := GetContentPrompt(0, "twitter", "short", "casual", "tech")
	assert.True(t, strings.HasPrefix(contentPrompt, base), "content prompt should start with the chat prompt")
	assert.Contains(t, contentPrompt, "CONTENT GENERATION MODE")
}

// #309: the content prompt keeps the persona as its Static part, the
// content-mode addendum in Dynamic, and the same whole bytes as
// GetContentPrompt, so an Anthropic request caches the persona apart.
func TestContentPromptKeepsThePersonaStatic(t *testing.T) {
	composeEnv(t, false)
	p := ContentPrompt(0, "twitter", "short", "playful", "a launch")
	if p.Static != mustProfile(ProfileFull).SystemPrompt {
		t.Fatal("Static is not the full profile")
	}
	if !strings.Contains(p.Dynamic, "CONTENT GENERATION MODE") || strings.Contains(p.Static, "CONTENT GENERATION MODE") {
		t.Fatal("the content addendum is not in Dynamic")
	}
	if p.String() != GetContentPrompt(0, "twitter", "short", "playful", "a launch") {
		t.Fatal("ContentPrompt and GetContentPrompt disagree")
	}
}
