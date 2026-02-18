package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadEssence tests loading persona essence from embedded data
func TestLoadEssence(t *testing.T) {
	essence, err := LoadEssence()
	require.NoError(t, err, "Should load embedded essence")
	assert.NotNil(t, essence, "Essence should not be nil")

	// Verify essential fields are populated
	assert.NotEmpty(t, essence.Version, "Version should be set")
	assert.NotEmpty(t, essence.Character, "Character should be set")
	assert.NotEmpty(t, essence.Description, "Description should be set")
	assert.NotEmpty(t, essence.Voice.Style, "Voice style should be set")
	assert.NotEmpty(t, essence.CoreRules, "Core rules should be set")
}

// TestLoadEssenceFromFile tests loading essence from custom file
func TestLoadEssenceFromFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping HOME environment variable test on Windows")
	}

	// Create temp directory for test config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".celeste")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Create custom essence file
	customEssence := CelesteEssence{
		Version:     "test-1.0",
		Character:   "Test Celeste",
		Description: "A test version",
		CoreRules:   []string{"Test rule 1", "Test rule 2"},
	}
	customEssence.Voice.Style = "Test style"

	data, err := json.MarshalIndent(customEssence, "", "  ")
	require.NoError(t, err)

	essencePath := filepath.Join(configDir, "celeste_essence.json")
	require.NoError(t, os.WriteFile(essencePath, data, 0644))

	// Temporarily change home dir
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Load essence (should use custom file)
	essence, err := LoadEssence()
	require.NoError(t, err)
	assert.Equal(t, "test-1.0", essence.Version, "Should load custom version")
	assert.Equal(t, "Test Celeste", essence.Character, "Should load custom character")
}

// TestGetSystemPrompt tests system prompt generation
func TestGetSystemPrompt(t *testing.T) {
	t.Run("with prompt", func(t *testing.T) {
		prompt := GetSystemPrompt(false)
		assert.NotEmpty(t, prompt, "Prompt should not be empty")
		assert.Contains(t, prompt, "Celeste", "Prompt should mention Celeste")
	})

	t.Run("skip prompt", func(t *testing.T) {
		prompt := GetSystemPrompt(true)
		assert.Empty(t, prompt, "Prompt should be empty when skipped")
	})
}

// TestBuildPromptFromEssence tests prompt construction
func TestBuildPromptFromEssence(t *testing.T) {
	essence := &CelesteEssence{
		Version:          "1.0",
		Character:        "Test Character",
		Description:      "A test character description",
		CoreRules:        []string{"Rule 1", "Rule 2"},
		InteractionRules: []string{"Interaction rule 1"},
		KnowledgeUsage:   "Test knowledge usage",
	}
	essence.Voice.Style = "Test voice style"
	essence.Voice.Constraints = []string{"Constraint 1", "Constraint 2"}
	essence.Safety.PlatformSafety = "Test safety"
	essence.Safety.RefuseList = []string{"refuse1", "refuse2"}
	essence.Safety.SafeAlternatives = "Test alternatives"

	prompt := buildPromptFromEssence(essence)

	// Verify all sections are present
	assert.Contains(t, prompt, "Test Character", "Should include character")
	assert.Contains(t, prompt, "A test character description", "Should include description")
	assert.Contains(t, prompt, "Test voice style", "Should include voice style")
	assert.Contains(t, prompt, "Constraint 1", "Should include voice constraints")
	assert.Contains(t, prompt, "Rule 1", "Should include core rules")
	assert.Contains(t, prompt, "Interaction rule 1", "Should include interaction rules")
	assert.Contains(t, prompt, "Test safety", "Should include safety")
	assert.Contains(t, prompt, "refuse1, refuse2", "Should include refuse list")
	assert.Contains(t, prompt, "Test alternatives", "Should include safe alternatives")
	assert.Contains(t, prompt, "Test knowledge usage", "Should include knowledge usage")
}

// TestBuildPromptWithMinimalEssence tests prompt with minimal data
func TestBuildPromptWithMinimalEssence(t *testing.T) {
	essence := &CelesteEssence{
		Character:   "Minimal",
		Description: "Minimal description",
	}

	prompt := buildPromptFromEssence(essence)
	assert.Contains(t, prompt, "Minimal", "Should include minimal character")
	assert.Contains(t, prompt, "Minimal description", "Should include minimal description")
}

// TestGetBasicPrompt tests fallback prompt
func TestGetBasicPrompt(t *testing.T) {
	prompt := getBasicPrompt()
	assert.NotEmpty(t, prompt, "Basic prompt should not be empty")
	assert.Contains(t, prompt, "Celeste", "Should mention Celeste")
	assert.Contains(t, prompt, "demon noble VTuber", "Should describe character type")
	assert.Contains(t, prompt, "Voice Style", "Should include voice style")
	assert.Contains(t, prompt, "Core Rules", "Should include core rules")
	assert.Contains(t, prompt, "Safety", "Should include safety")
}

// TestGetNSFWPrompt tests NSFW mode prompt
func TestGetNSFWPrompt(t *testing.T) {
	prompt := GetNSFWPrompt()
	assert.NotEmpty(t, prompt, "NSFW prompt should not be empty")
	assert.Contains(t, prompt, "NSFW MODE ACTIVE", "Should indicate NSFW mode")
	assert.Contains(t, prompt, "content restrictions are lifted", "Should mention lifted restrictions")
	assert.Contains(t, prompt, "Still refuse", "Should still include safety rules")
	assert.Contains(t, prompt, "Venice.ai", "Should mention Venice.ai")
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

// TestPromptStructure tests that prompts have expected structure
func TestPromptStructure(t *testing.T) {
	prompt := GetSystemPrompt(false)

	// Check for essential sections
	expectedSections := []string{
		"Voice",
		"Core Rules",
		"Safety",
	}

	for _, section := range expectedSections {
		assert.Contains(t, prompt, section, "Prompt should contain section: %s", section)
	}
}

// TestEssenceValidation tests that loaded essence has valid structure
func TestEssenceValidation(t *testing.T) {
	essence, err := LoadEssence()
	require.NoError(t, err)

	// Validate Voice structure
	assert.NotEmpty(t, essence.Voice.Style, "Voice style should be set")
	assert.NotEmpty(t, essence.Voice.EmojiUsage, "Emoji usage should be defined")

	// Validate Safety structure
	assert.NotEmpty(t, essence.Safety.PlatformSafety, "Platform safety should be defined")
	assert.NotEmpty(t, essence.Safety.RefuseList, "Refuse list should be defined")

	// Validate OperationalLaws
	assert.NotEmpty(t, essence.OperationalLaws, "Operational laws should be defined")
}

// TestGetSystemPromptConsistency tests that repeated calls return same result
func TestGetSystemPromptConsistency(t *testing.T) {
	prompt1 := GetSystemPrompt(false)
	prompt2 := GetSystemPrompt(false)

	assert.Equal(t, prompt1, prompt2, "Multiple calls should return identical prompts")
}

// TestNSFWPromptIncludesBase tests that NSFW prompt includes base prompt
func TestNSFWPromptIncludesBase(t *testing.T) {
	basePrompt := GetSystemPrompt(false)
	nsfwPrompt := GetNSFWPrompt()

	assert.Contains(t, nsfwPrompt, strings.TrimSpace(basePrompt),
		"NSFW prompt should include base prompt")
	assert.Greater(t, len(nsfwPrompt), len(basePrompt),
		"NSFW prompt should be longer than base prompt")
}

// TestContentPromptIncludesBase tests that content prompt includes base prompt
func TestContentPromptIncludesBase(t *testing.T) {
	basePrompt := GetSystemPrompt(false)
	contentPrompt := GetContentPrompt("twitter", "short", "casual", "tech")

	assert.Contains(t, contentPrompt, strings.TrimSpace(basePrompt),
		"Content prompt should include base prompt")
	assert.Greater(t, len(contentPrompt), len(basePrompt),
		"Content prompt should be longer than base prompt")
}
