package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig tests that default config has sensible values
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config)
	assert.Equal(t, "https://api.openai.com/v1", config.BaseURL)
	assert.Equal(t, "gpt-4o-mini", config.Model)
	assert.Equal(t, 60, config.Timeout)
	assert.False(t, config.SkipPersonaPrompt)
	assert.True(t, config.SimulateTyping)
	assert.Equal(t, 40, config.TypingSpeed)
	assert.Equal(t, "https://api.venice.ai/api/v1", config.VeniceBaseURL)
	assert.Equal(t, "venice-uncensored", config.VeniceModel)
}

// TestPaths tests config path generation
func TestPaths(t *testing.T) {
	configDir, configFile, secretsFile, skillsFile := Paths()

	homeDir, _ := os.UserHomeDir()
	expectedDir := filepath.Join(homeDir, ".celeste")

	assert.Equal(t, expectedDir, configDir)
	assert.Equal(t, filepath.Join(expectedDir, "config.json"), configFile)
	assert.Equal(t, filepath.Join(expectedDir, "secrets.json"), secretsFile)
	assert.Equal(t, filepath.Join(expectedDir, "skills.json"), skillsFile)
}

// TestNamedConfigPath tests named config path generation
func TestNamedConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty name returns default",
			input:    "",
			expected: "config.json",
		},
		{
			name:     "named config",
			input:    "openai",
			expected: "config.openai.json",
		},
		{
			name:     "named config with hyphen",
			input:    "my-special-config",
			expected: "config.my-special-config.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NamedConfigPath(tt.input)
			assert.Contains(t, result, tt.expected)
			assert.Contains(t, result, ".celeste")
		})
	}
}

// TestSaveAndLoad tests config save/load roundtrip
func TestSaveAndLoad(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	// Override home directory for testing (set both HOME and USERPROFILE for Windows)
	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir) // Windows uses USERPROFILE

	// Unset environment variables to prevent Docker compose env vars from polluting tests
	oldAPIKey := os.Getenv("CELESTE_API_KEY")
	oldEndpoint := os.Getenv("CELESTE_API_ENDPOINT")
	defer func() {
		if oldAPIKey != "" {
			os.Setenv("CELESTE_API_KEY", oldAPIKey)
		}
		if oldEndpoint != "" {
			os.Setenv("CELESTE_API_ENDPOINT", oldEndpoint)
		}
	}()
	os.Unsetenv("CELESTE_API_KEY")
	os.Unsetenv("CELESTE_API_ENDPOINT")

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create config
	config := &Config{
		APIKey:            "test-api-key",
		BaseURL:           "https://test.example.com",
		Model:             "test-model",
		Timeout:           120,
		SkipPersonaPrompt: true,
		SimulateTyping:    false,
		TypingSpeed:       50,
	}

	// Save config
	err = Save(config)
	require.NoError(t, err)

	// Load config
	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// Verify values
	assert.Equal(t, config.APIKey, loaded.APIKey)
	assert.Equal(t, config.BaseURL, loaded.BaseURL)
	assert.Equal(t, config.Model, loaded.Model)
	assert.Equal(t, config.Timeout, loaded.Timeout)
	assert.Equal(t, config.SkipPersonaPrompt, loaded.SkipPersonaPrompt)
	assert.Equal(t, config.SimulateTyping, loaded.SimulateTyping)
	assert.Equal(t, config.TypingSpeed, loaded.TypingSpeed)
}

// TestLoadSkillsConfig tests loading skills configuration
func TestLoadSkillsConfig(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Test 1: No skills.json (should return empty config)
	skillsConfig, err := LoadSkillsConfig()
	require.NoError(t, err)
	assert.NotNil(t, skillsConfig)

	// Test 2: Create skills.json and load it
	skillsData := map[string]interface{}{
		"venice_api_key":           "test-venice-key",
		"tarot_auth_token":         "test-tarot-token",
		"weather_default_zip_code": "12345",
		"twitch_client_id":         "test-twitch-id",
		"youtube_api_key":          "test-youtube-key",
	}

	skillsJSON, err := json.MarshalIndent(skillsData, "", "  ")
	require.NoError(t, err)

	skillsFile := filepath.Join(configDir, "skills.json")
	err = os.WriteFile(skillsFile, skillsJSON, 0600)
	require.NoError(t, err)

	// Load skills config
	skillsConfig, err = LoadSkillsConfig()
	require.NoError(t, err)
	assert.Equal(t, "test-venice-key", skillsConfig.VeniceAPIKey)
	assert.Equal(t, "test-tarot-token", skillsConfig.TarotAuthToken)
	assert.Equal(t, "12345", skillsConfig.WeatherDefaultZipCode)
	assert.Equal(t, "test-twitch-id", skillsConfig.TwitchClientID)
	assert.Equal(t, "test-youtube-key", skillsConfig.YouTubeAPIKey)
}

// TestSaveSkillsConfig tests saving skills configuration
func TestSaveSkillsConfig(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create config with skill settings
	config := &Config{
		VeniceAPIKey:          "test-venice-key",
		TarotAuthToken:        "test-tarot-token",
		WeatherDefaultZipCode: "10001",
		TwitchClientID:        "test-twitch-id",
		YouTubeAPIKey:         "test-youtube-key",
		// Non-skill fields (should not be saved)
		APIKey:  "main-api-key",
		BaseURL: "https://test.com",
	}

	// Save skills config
	err = SaveSkillsConfig(config)
	require.NoError(t, err)

	// Verify file exists with correct permissions (Unix-only check)
	skillsFile := filepath.Join(configDir, "skills.json")
	info, err := os.Stat(skillsFile)
	require.NoError(t, err)
	// Skip permission check on Windows (Windows doesn't use Unix permission bits)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	// Load and verify
	data, err := os.ReadFile(skillsFile)
	require.NoError(t, err)

	var saved map[string]interface{}
	err = json.Unmarshal(data, &saved)
	require.NoError(t, err)

	// Check skill fields are present
	assert.Equal(t, "test-venice-key", saved["venice_api_key"])
	assert.Equal(t, "test-tarot-token", saved["tarot_auth_token"])
	assert.Equal(t, "10001", saved["weather_default_zip_code"])

	// Check non-skill fields are either empty or not set meaningfully
	// (JSON will include zero values, but they should be empty)
	if apiKey, ok := saved["api_key"]; ok {
		assert.Empty(t, apiKey, "api_key should be empty in skills config")
	}
	if baseURL, ok := saved["base_url"]; ok {
		assert.Empty(t, baseURL, "base_url should be empty in skills config")
	}
}

// TestLoadNamed tests named config loading
func TestLoadNamed(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Unset environment variables to prevent Docker compose env vars from polluting tests
	oldAPIKey := os.Getenv("CELESTE_API_KEY")
	oldEndpoint := os.Getenv("CELESTE_API_ENDPOINT")
	defer func() {
		if oldAPIKey != "" {
			os.Setenv("CELESTE_API_KEY", oldAPIKey)
		}
		if oldEndpoint != "" {
			os.Setenv("CELESTE_API_ENDPOINT", oldEndpoint)
		}
	}()
	os.Unsetenv("CELESTE_API_KEY")
	os.Unsetenv("CELESTE_API_ENDPOINT")

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create a named config file
	namedConfig := &Config{
		APIKey:  "named-api-key",
		BaseURL: "https://named.example.com",
		Model:   "named-model",
		Timeout: 90,
	}

	namedPath := filepath.Join(configDir, "config.openai.json")
	data, err := json.MarshalIndent(namedConfig, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(namedPath, data, 0644)
	require.NoError(t, err)

	// Load named config
	loaded, err := LoadNamed("openai")
	require.NoError(t, err)
	assert.Equal(t, "named-api-key", loaded.APIKey)
	assert.Equal(t, "https://named.example.com", loaded.BaseURL)
	assert.Equal(t, "named-model", loaded.Model)
	assert.Equal(t, 90, loaded.Timeout)

	// Test nonexistent config
	_, err = LoadNamed("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestLoadNamedWithSkillsMerge tests that skills.json merges with named configs
func TestLoadNamedWithSkillsMerge(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Unset environment variables to prevent Docker compose env vars from polluting tests
	oldAPIKey := os.Getenv("CELESTE_API_KEY")
	oldEndpoint := os.Getenv("CELESTE_API_ENDPOINT")
	oldVeniceKey := os.Getenv("VENICE_API_KEY")
	oldTarotToken := os.Getenv("TAROT_AUTH_TOKEN")
	defer func() {
		if oldAPIKey != "" {
			os.Setenv("CELESTE_API_KEY", oldAPIKey)
		}
		if oldEndpoint != "" {
			os.Setenv("CELESTE_API_ENDPOINT", oldEndpoint)
		}
		if oldVeniceKey != "" {
			os.Setenv("VENICE_API_KEY", oldVeniceKey)
		}
		if oldTarotToken != "" {
			os.Setenv("TAROT_AUTH_TOKEN", oldTarotToken)
		}
	}()
	os.Unsetenv("CELESTE_API_KEY")
	os.Unsetenv("CELESTE_API_ENDPOINT")
	os.Unsetenv("VENICE_API_KEY")
	os.Unsetenv("TAROT_AUTH_TOKEN")

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create skills.json
	skillsData := map[string]interface{}{
		"venice_api_key":   "skills-venice-key",
		"tarot_auth_token": "skills-tarot-token",
	}
	skillsJSON, err := json.MarshalIndent(skillsData, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(configDir, "skills.json"), skillsJSON, 0600)
	require.NoError(t, err)

	// Create named config (without skill fields)
	namedConfig := &Config{
		APIKey:  "named-key",
		BaseURL: "https://named.com",
	}
	namedPath := filepath.Join(configDir, "config.test.json")
	data, err := json.MarshalIndent(namedConfig, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(namedPath, data, 0644)
	require.NoError(t, err)

	// Load named config
	loaded, err := LoadNamed("test")
	require.NoError(t, err)

	// Verify main config fields
	assert.Equal(t, "named-key", loaded.APIKey)
	assert.Equal(t, "https://named.com", loaded.BaseURL)

	// Verify skill fields from skills.json were merged
	assert.Equal(t, "skills-venice-key", loaded.VeniceAPIKey)
	assert.Equal(t, "skills-tarot-token", loaded.TarotAuthToken)
}

// TestListConfigs tests listing available configs
func TestListConfigs(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Test empty directory
	configs, err := ListConfigs()
	require.NoError(t, err)
	assert.Empty(t, configs)

	// Create config files
	files := []string{
		"config.json",        // default
		"config.openai.json", // named
		"config.grok.json",   // named
		"config.venice.json", // named
		"skills.json",        // not a config
		"sessions/test.json", // not a config
	}

	for _, file := range files {
		path := filepath.Join(configDir, file)
		if filepath.Dir(file) != "." {
			os.MkdirAll(filepath.Dir(path), 0755)
		}
		os.WriteFile(path, []byte("{}"), 0644)
	}

	// List configs
	configs, err = ListConfigs()
	require.NoError(t, err)
	assert.Len(t, configs, 4, "should find 4 configs")

	// Verify config names
	assert.Contains(t, configs, "default")
	assert.Contains(t, configs, "openai")
	assert.Contains(t, configs, "grok")
	assert.Contains(t, configs, "venice")
}

// TestConfigLoader tests the ConfigLoader interface implementation
func TestConfigLoader(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	homeDir := tmpDir

	oldHomeDir := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	defer func() {
		os.Setenv("HOME", oldHomeDir)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)

	// Unset environment variables to prevent Docker compose env vars from polluting tests
	oldVeniceKey := os.Getenv("VENICE_API_KEY")
	oldTarotToken := os.Getenv("TAROT_AUTH_TOKEN")
	defer func() {
		if oldVeniceKey != "" {
			os.Setenv("VENICE_API_KEY", oldVeniceKey)
		}
		if oldTarotToken != "" {
			os.Setenv("TAROT_AUTH_TOKEN", oldTarotToken)
		}
	}()
	os.Unsetenv("VENICE_API_KEY")
	os.Unsetenv("TAROT_AUTH_TOKEN")

	// Create .celeste directory
	configDir := filepath.Join(homeDir, ".celeste")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create config with skill settings
	config := &Config{
		VeniceAPIKey:          "venice-key",
		VeniceBaseURL:         "https://venice.example.com",
		VeniceModel:           "venice-model",
		TarotFunctionURL:      "https://tarot.example.com",
		TarotAuthToken:        "tarot-token",
		WeatherDefaultZipCode: "90210",
		TwitchClientID:        "twitch-id",
		TwitchDefaultStreamer: "test-streamer",
		YouTubeAPIKey:         "youtube-key",
		YouTubeDefaultChannel: "test-channel",
	}

	// Save config
	err = Save(config)
	require.NoError(t, err)

	// Load and create ConfigLoader
	loaded, err := Load()
	require.NoError(t, err)

	loader := NewConfigLoader(loaded)
	require.NotNil(t, loader)

	// Test GetVeniceConfig
	veniceConfig, err := loader.GetVeniceConfig()
	require.NoError(t, err)
	assert.Equal(t, "venice-key", veniceConfig.APIKey)
	assert.Equal(t, "https://venice.example.com", veniceConfig.BaseURL)
	assert.Equal(t, "venice-model", veniceConfig.Model)

	// Test GetTarotConfig
	tarotConfig, err := loader.GetTarotConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://tarot.example.com", tarotConfig.FunctionURL)
	assert.Equal(t, "tarot-token", tarotConfig.AuthToken)

	// Test GetWeatherConfig
	weatherConfig, err := loader.GetWeatherConfig()
	require.NoError(t, err)
	assert.Equal(t, "90210", weatherConfig.DefaultZipCode)

	// Test GetTwitchConfig
	twitchConfig, err := loader.GetTwitchConfig()
	require.NoError(t, err)
	assert.Equal(t, "twitch-id", twitchConfig.ClientID)
	assert.Equal(t, "test-streamer", twitchConfig.DefaultStreamer)

	// Test GetYouTubeConfig
	youtubeConfig, err := loader.GetYouTubeConfig()
	require.NoError(t, err)
	assert.Equal(t, "youtube-key", youtubeConfig.APIKey)
	assert.Equal(t, "test-channel", youtubeConfig.DefaultChannel)
}
