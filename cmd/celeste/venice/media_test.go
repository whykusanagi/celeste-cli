package venice

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseMediaCommand tests parsing various media command formats
func TestParseMediaCommand(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectType    string
		expectPrompt  string
		expectParams  map[string]interface{}
		expectIsMedia bool
	}{
		{
			name:          "Anime shortcut",
			input:         "anime: cute girl with purple hair",
			expectType:    "image",
			expectPrompt:  "cute girl with purple hair",
			expectParams:  map[string]interface{}{"model": "wai-Illustrious", "steps": 30},
			expectIsMedia: true,
		},
		{
			name:          "Dream shortcut",
			input:         "dream: fantasy landscape",
			expectType:    "image",
			expectPrompt:  "fantasy landscape",
			expectParams:  map[string]interface{}{"model": "hidream", "steps": 30},
			expectIsMedia: true,
		},
		{
			name:          "Custom model syntax",
			input:         "image[pixart-a]: beautiful sunset",
			expectType:    "image",
			expectPrompt:  "beautiful sunset",
			expectParams:  map[string]interface{}{"model": "pixart-a"},
			expectIsMedia: true,
		},
		{
			name:          "Standard image prefix",
			input:         "image: a red car",
			expectType:    "image",
			expectPrompt:  "a red car",
			expectParams:  map[string]interface{}{},
			expectIsMedia: true,
		},
		{
			name:          "Upscale prefix with path",
			input:         "upscale: /path/to/image.png some params",
			expectType:    "upscale",
			expectPrompt:  "some params",
			expectParams:  map[string]interface{}{"path": "/path/to/image.png"},
			expectIsMedia: true,
		},
		{
			name:          "Upscale with just path",
			input:         "upscale: /path/to/image.png",
			expectType:    "upscale",
			expectPrompt:  "",
			expectParams:  map[string]interface{}{"path": "/path/to/image.png"},
			expectIsMedia: true,
		},
		{
			name:          "Not a media command",
			input:         "Tell me a joke",
			expectType:    "",
			expectPrompt:  "",
			expectParams:  nil,
			expectIsMedia: false,
		},
		{
			name:          "Anime with uppercase",
			input:         "ANIME: Test prompt",
			expectType:    "image",
			expectPrompt:  "Test prompt",
			expectParams:  map[string]interface{}{"model": "wai-Illustrious", "steps": 30},
			expectIsMedia: true,
		},
		{
			name:          "Image with leading/trailing spaces",
			input:         "  image:   test prompt   ",
			expectType:    "image",
			expectPrompt:  "test prompt",
			expectParams:  map[string]interface{}{},
			expectIsMedia: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaType, prompt, params, isMedia := ParseMediaCommand(tt.input)

			assert.Equal(t, tt.expectIsMedia, isMedia, "isMedia should match")
			if tt.expectIsMedia {
				assert.Equal(t, tt.expectType, mediaType, "Media type should match")
				assert.Equal(t, tt.expectPrompt, prompt, "Prompt should match")

				// Check expected params
				for key, expectedVal := range tt.expectParams {
					actualVal, ok := params[key]
					assert.True(t, ok, "Param %s should exist", key)
					assert.Equal(t, expectedVal, actualVal, "Param %s should match", key)
				}
			}
		})
	}
}

// TestParseMediaCommandEdgeCases tests edge cases
func TestParseMediaCommandEdgeCases(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		_, _, _, isMedia := ParseMediaCommand("")
		assert.False(t, isMedia, "Empty string should not be media command")
	})

	t.Run("only prefix", func(t *testing.T) {
		mediaType, prompt, params, isMedia := ParseMediaCommand("image:")
		assert.True(t, isMedia, "Should recognize as media command")
		assert.Equal(t, "image", mediaType)
		assert.Empty(t, prompt, "Prompt should be empty")
		assert.NotNil(t, params)
	})

	t.Run("malformed custom model", func(t *testing.T) {
		// Missing closing bracket
		_, _, _, isMedia := ParseMediaCommand("image[model: prompt")
		assert.False(t, isMedia, "Malformed syntax should not match")
	})

	t.Run("anime without colon", func(t *testing.T) {
		_, _, _, isMedia := ParseMediaCommand("anime prompt")
		assert.False(t, isMedia, "Should require colon")
	})
}

// TestGetDownloadsDir tests downloads directory resolution
func TestGetDownloadsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping HOME directory tests on Windows")
	}

	t.Run("default downloads directory", func(t *testing.T) {
		// Create temp home directory
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		dir := getDownloadsDir()
		expectedDir := filepath.Join(tmpHome, "Downloads")
		assert.Equal(t, expectedDir, dir, "Should return ~/Downloads by default")
	})

	t.Run("with skills.json config", func(t *testing.T) {
		// Create temp home with config
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		// Create .celeste directory
		celesteDir := filepath.Join(tmpHome, ".celeste")
		require.NoError(t, os.MkdirAll(celesteDir, 0755))

		// Create skills.json with custom downloads_dir
		customDir := filepath.Join(tmpHome, "CustomDownloads")
		skillsConfig := []byte(`{"downloads_dir": "` + customDir + `"}`)
		skillsPath := filepath.Join(celesteDir, "skills.json")
		require.NoError(t, os.WriteFile(skillsPath, skillsConfig, 0644))

		dir := getDownloadsDir()
		assert.Equal(t, customDir, dir, "Should use custom directory from config")
	})

	t.Run("with tilde expansion", func(t *testing.T) {
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		celesteDir := filepath.Join(tmpHome, ".celeste")
		require.NoError(t, os.MkdirAll(celesteDir, 0755))

		// Config with ~ path
		skillsConfig := []byte(`{"downloads_dir": "~/MyImages"}`)
		skillsPath := filepath.Join(celesteDir, "skills.json")
		require.NoError(t, os.WriteFile(skillsPath, skillsConfig, 0644))

		dir := getDownloadsDir()
		expectedDir := filepath.Join(tmpHome, "MyImages")
		assert.Equal(t, expectedDir, dir, "Should expand ~ to home directory")
	})

	t.Run("with invalid JSON", func(t *testing.T) {
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		celesteDir := filepath.Join(tmpHome, ".celeste")
		require.NoError(t, os.MkdirAll(celesteDir, 0755))

		// Invalid JSON
		skillsPath := filepath.Join(celesteDir, "skills.json")
		require.NoError(t, os.WriteFile(skillsPath, []byte("invalid json"), 0644))

		dir := getDownloadsDir()
		// Should fall back to default
		expectedDir := filepath.Join(tmpHome, "Downloads")
		assert.Equal(t, expectedDir, dir, "Should fall back to default on invalid JSON")
	})
}

// TestSaveBase64Image tests image saving functionality
func TestSaveBase64Image(t *testing.T) {
	t.Run("valid base64 image", func(t *testing.T) {
		// Create temp home directory
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		// Create a simple 1x1 red PNG image in base64
		// This is a minimal valid PNG
		pngData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
			0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
			0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
			0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
			0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
			0x44, 0xAE, 0x42, 0x60, 0x82,
		}

		b64 := base64.StdEncoding.EncodeToString(pngData)

		path, err := saveBase64Image(b64, "test")
		require.NoError(t, err, "Should save image successfully")
		assert.NotEmpty(t, path, "Path should not be empty")

		// Verify file exists
		_, err = os.Stat(path)
		assert.NoError(t, err, "File should exist")

		// Verify file content
		savedData, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, pngData, savedData, "Saved data should match original")

		// Verify filename format
		filename := filepath.Base(path)
		assert.True(t, strings.HasPrefix(filename, "celeste_test_"), "Filename should have correct prefix")
		assert.True(t, strings.HasSuffix(filename, ".png"), "Filename should have .png extension")
	})

	t.Run("invalid base64", func(t *testing.T) {
		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		_, err := saveBase64Image("invalid!base64!", "test")
		assert.Error(t, err, "Should return error for invalid base64")
		assert.Contains(t, err.Error(), "failed to decode", "Error should mention decode failure")
	})

	t.Run("creates downloads directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Skipping HOME directory test on Windows")
		}

		tmpHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", originalHome)

		// Downloads directory should not exist yet
		downloadsDir := filepath.Join(tmpHome, "Downloads")
		_, err := os.Stat(downloadsDir)
		assert.True(t, os.IsNotExist(err), "Downloads dir should not exist initially")

		// Save an image (will create directory)
		validPNG := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
		_, _ = saveBase64Image(validPNG, "test") // May error on invalid PNG, but directory should be created
		_, statErr := os.Stat(downloadsDir)
		assert.NoError(t, statErr, "Downloads dir should be created")
	})
}

// TestConfigStructure tests Config struct initialization
func TestConfigStructure(t *testing.T) {
	config := Config{
		APIKey:  "test-key",
		BaseURL: "https://test.api",
		Model:   "test-model",
	}

	assert.Equal(t, "test-key", config.APIKey)
	assert.Equal(t, "https://test.api", config.BaseURL)
	assert.Equal(t, "test-model", config.Model)
}

// TestMediaRequestStructure tests MediaRequest struct
func TestMediaRequestStructure(t *testing.T) {
	req := MediaRequest{
		Type:   "image",
		Prompt: "test prompt",
		Params: map[string]interface{}{
			"width":  1024,
			"height": 1024,
		},
	}

	assert.Equal(t, "image", req.Type)
	assert.Equal(t, "test prompt", req.Prompt)
	assert.Equal(t, 1024, req.Params["width"])
	assert.Equal(t, 1024, req.Params["height"])
}

// TestMediaResponseStructure tests MediaResponse struct
func TestMediaResponseStructure(t *testing.T) {
	resp := MediaResponse{
		Success:   true,
		URL:       "https://example.com/image.png",
		Path:      "/path/to/image.png",
		Error:     "",
		MediaType: "image",
	}

	assert.True(t, resp.Success)
	assert.Equal(t, "https://example.com/image.png", resp.URL)
	assert.Equal(t, "/path/to/image.png", resp.Path)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "image", resp.MediaType)
}

// TestParseMediaCommandCaseSensitivity tests case handling
func TestParseMediaCommandCaseSensitivity(t *testing.T) {
	variations := []string{
		"anime: test",
		"ANIME: test",
		"Anime: test",
		"AnImE: test",
	}

	for _, input := range variations {
		t.Run(input, func(t *testing.T) {
			mediaType, prompt, params, isMedia := ParseMediaCommand(input)
			assert.True(t, isMedia, "Should recognize regardless of case")
			assert.Equal(t, "image", mediaType)
			assert.Equal(t, "test", prompt)
			assert.Equal(t, "wai-Illustrious", params["model"])
		})
	}
}

// TestParseMediaCommandWithSpecialCharacters tests special character handling
func TestParseMediaCommandWithSpecialCharacters(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"Unicode", "anime: 日本語 プロンプト", "日本語 プロンプト"},
		{"Emojis", "anime: test 😊 🎨", "test 😊 🎨"},
		{"Special chars", "anime: test@#$%", "test@#$%"},
		{"Newlines", "anime: test\nwith newline", "test\nwith newline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, prompt, _, isMedia := ParseMediaCommand(tt.input)
			assert.True(t, isMedia)
			assert.Equal(t, tt.expect, prompt)
		})
	}
}
