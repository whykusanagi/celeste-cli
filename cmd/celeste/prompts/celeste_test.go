package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetSystemPrompt tests system prompt generation
func TestGetSystemPrompt(t *testing.T) {
	prompt := GetSystemPrompt()
	assert.NotEmpty(t, prompt, "Prompt should not be empty")
	assert.Contains(t, prompt, "Celeste", "Prompt should mention Celeste")
}

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
			prompt := GetContentPrompt(tt.platform, tt.format, tt.tone, tt.topic)

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
			prompt := GetContentPrompt(p.name, "", "", "")
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
			prompt := GetContentPrompt("", f.name, "", "")
			assert.Contains(t, prompt, f.keyword, "Should mention format")
		})
	}
}

// TestGetSystemPromptConsistency tests that repeated calls return same result
func TestGetSystemPromptConsistency(t *testing.T) {
	prompt1 := GetSystemPrompt()
	prompt2 := GetSystemPrompt()

	assert.Equal(t, prompt1, prompt2, "Multiple calls should return identical prompts")
}

// TestContentPromptIncludesBase tests that content prompt includes base prompt
func TestContentPromptIncludesBase(t *testing.T) {
	basePrompt := GetSystemPrompt()
	contentPrompt := GetContentPrompt("twitter", "short", "casual", "tech")

	assert.Contains(t, contentPrompt, strings.TrimSpace(basePrompt),
		"Content prompt should include base prompt")
	assert.Greater(t, len(contentPrompt), len(basePrompt),
		"Content prompt should be longer than base prompt")
}
