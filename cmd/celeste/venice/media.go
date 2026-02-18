// Package venice provides Venice.ai API integration for media generation.
// This handles image, video, upscaling, and image-to-video generation
// that cannot work through function calling (uncensored models).
package venice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds Venice.ai API configuration.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string // For image generation: fluently-xl, pixart-a, etc.
}

// MediaRequest represents a media generation request.
type MediaRequest struct {
	Type   string // "image", "video", "upscale", "image-to-video"
	Prompt string
	Params map[string]interface{}
}

// MediaResponse represents the response from media generation.
type MediaResponse struct {
	Success   bool   `json:"success"`
	URL       string `json:"url,omitempty"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
	MediaType string `json:"media_type"`
}

// GenerateImage generates an image using Venice.ai.
func GenerateImage(config Config, prompt string, params map[string]interface{}) (*MediaResponse, error) {
	// Use Venice's full-featured image generation endpoint
	url := config.BaseURL + "/image/generate"

	// Default parameters
	// Use image generation model from config, or default to lustify-sdxl
	model := config.Model
	if model == "" || model == "venice-uncensored" {
		// If no model specified or using chat model, default to image generation model
		model = "lustify-sdxl"
	}
	if m, ok := params["model"].(string); ok {
		model = m
	}

	// Width and height (1-1280, default 1024)
	width := 1024
	if w, ok := params["width"].(int); ok {
		width = w
	}

	height := 1024
	if h, ok := params["height"].(int); ok {
		height = h
	}

	// Steps (1-50, default 40 for high quality)
	// Note: Some models have lower limits (e.g., wai-Illustrious max is 30)
	steps := 40
	if s, ok := params["steps"].(int); ok {
		steps = s
	}

	// Apply model-specific step limits to prevent API errors
	modelStepLimits := map[string]int{
		"wai-Illustrious": 30,
		"hidream":         30,
		"nano-banana-pro": 30,
		"qwen-image":      30,
	}

	if maxSteps, hasLimit := modelStepLimits[model]; hasLimit {
		if steps > maxSteps {
			steps = maxSteps
		}
	}

	// CFG scale (0 < value <= 20, default 12 for strong prompt adherence)
	cfgScale := 12.0
	if cfg, ok := params["cfg_scale"].(float64); ok {
		cfgScale = cfg
	}

	// Number of variants (1-4, default 1)
	variants := 1
	if v, ok := params["variants"].(int); ok {
		variants = v
	}

	// Output format (png for best quality)
	format := "png"
	if f, ok := params["format"].(string); ok {
		format = f
	}

	// Safe mode disabled for NSFW content
	safeMode := false
	if sm, ok := params["safe_mode"].(bool); ok {
		safeMode = sm
	}

	// Build request payload according to Venice /image/generate API
	payload := map[string]interface{}{
		"model":     model,
		"prompt":    prompt,
		"width":     width,
		"height":    height,
		"steps":     steps,
		"cfg_scale": cfgScale,
		"variants":  variants,
		"format":    format,
		"safe_mode": safeMode,
	}

	// Optional: negative prompt
	if negPrompt, ok := params["negative_prompt"].(string); ok && negPrompt != "" {
		payload["negative_prompt"] = negPrompt
	}

	// Optional: seed for reproducibility
	if seed, ok := params["seed"].(int); ok {
		payload["seed"] = seed
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return &MediaResponse{
			Success:   false,
			Error:     fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)),
			MediaType: "image",
		}, nil
	}

	// Parse response - Venice /image/generate returns {"id": "...", "images": ["base64..."]}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract base64 images from response
	if images, ok := result["images"].([]interface{}); ok && len(images) > 0 {
		// Venice returns base64 strings directly in the images array
		if b64, ok := images[0].(string); ok {
			// Save to file
			path, err := saveBase64Image(b64, "image")
			if err != nil {
				return nil, fmt.Errorf("failed to save image: %w", err)
			}
			return &MediaResponse{
				Success:   true,
				Path:      path,
				MediaType: "image",
			}, nil
		}
	}

	return &MediaResponse{
		Success:   false,
		Error:     "No image data in response",
		MediaType: "image",
	}, nil
}

// UpscaleImage upscales an image using Venice.ai.
func UpscaleImage(config Config, imagePath string, params map[string]interface{}) (*MediaResponse, error) {
	url := config.BaseURL + "/image/upscale"

	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	// Convert to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Default parameters
	scale := 2
	if s, ok := params["scale"].(int); ok {
		scale = s
	}

	creativity := 0.5
	if c, ok := params["creativity"].(float64); ok {
		creativity = c
	}

	// Build request payload
	payload := map[string]interface{}{
		"image":             imageBase64,
		"scale":             scale,
		"enhance":           true,
		"enhanceCreativity": creativity,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return &MediaResponse{
			Success:   false,
			Error:     fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)),
			MediaType: "upscale",
		}, nil
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract upscaled image
	if imageURL, ok := result["url"].(string); ok {
		return &MediaResponse{
			Success:   true,
			URL:       imageURL,
			MediaType: "upscale",
		}, nil
	}

	if b64, ok := result["image"].(string); ok {
		path, err := saveBase64Image(b64, "upscale")
		if err != nil {
			return nil, fmt.Errorf("failed to save upscaled image: %w", err)
		}
		return &MediaResponse{
			Success:   true,
			Path:      path,
			MediaType: "upscale",
		}, nil
	}

	return &MediaResponse{
		Success:   false,
		Error:     "No image data in response",
		MediaType: "upscale",
	}, nil
}

// GenerateVideo generates a video using Venice.ai.
func GenerateVideo(config Config, prompt string, params map[string]interface{}) (*MediaResponse, error) {
	url := config.BaseURL + "/videos/generations"

	// Default parameters
	duration := 5
	if d, ok := params["duration"].(int); ok {
		duration = d
	}

	fps := 24
	if f, ok := params["fps"].(int); ok {
		fps = f
	}

	// Build request payload
	payload := map[string]interface{}{
		"prompt":   prompt,
		"duration": duration,
		"fps":      fps,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second} // Longer timeout for video
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return &MediaResponse{
			Success:   false,
			Error:     fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)),
			MediaType: "video",
		}, nil
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract video URL
	if videoURL, ok := result["url"].(string); ok {
		return &MediaResponse{
			Success:   true,
			URL:       videoURL,
			MediaType: "video",
		}, nil
	}

	return &MediaResponse{
		Success:   false,
		Error:     "No video URL in response",
		MediaType: "video",
	}, nil
}

// ImageToVideo converts an image to video using Venice.ai.
func ImageToVideo(config Config, imagePath string, params map[string]interface{}) (*MediaResponse, error) {
	url := config.BaseURL + "/videos/image-to-video"

	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	// Convert to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Default parameters
	duration := 5
	if d, ok := params["duration"].(int); ok {
		duration = d
	}

	motion := "medium"
	if m, ok := params["motion"].(string); ok {
		motion = m
	}

	// Build request payload
	payload := map[string]interface{}{
		"image":    imageBase64,
		"duration": duration,
		"motion":   motion,
	}

	if prompt, ok := params["prompt"].(string); ok && prompt != "" {
		payload["prompt"] = prompt
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return &MediaResponse{
			Success:   false,
			Error:     fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)),
			MediaType: "image-to-video",
		}, nil
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract video URL
	if videoURL, ok := result["url"].(string); ok {
		return &MediaResponse{
			Success:   true,
			URL:       videoURL,
			MediaType: "image-to-video",
		}, nil
	}

	return &MediaResponse{
		Success:   false,
		Error:     "No video URL in response",
		MediaType: "image-to-video",
	}, nil
}

// saveBase64Image saves a base64 encoded image to disk.
func saveBase64Image(b64 string, prefix string) (string, error) {
	// Decode base64
	imgData, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Get output directory from config or use default
	outputDir := getDownloadsDir()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("celeste_%s_%s.png", prefix, timestamp)
	outputPath := filepath.Join(outputDir, filename)

	// Write file
	if err := os.WriteFile(outputPath, imgData, 0644); err != nil {
		return "", err
	}

	return outputPath, nil
}

// getDownloadsDir returns the downloads directory from config or default ~/Downloads
func getDownloadsDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}

	// Try to load from config
	configPath := filepath.Join(homeDir, ".celeste", "skills.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config map[string]interface{}
		if json.Unmarshal(data, &config) == nil {
			if dir, ok := config["downloads_dir"].(string); ok && dir != "" {
				// Expand ~ to home directory
				if strings.HasPrefix(dir, "~/") {
					return filepath.Join(homeDir, dir[2:])
				}
				return dir
			}
		}
	}

	// Default to ~/Downloads
	return filepath.Join(homeDir, "Downloads")
}

// ParseMediaCommand parses a message for media generation commands.
// Returns (type, prompt, params, isMediaCommand)
func ParseMediaCommand(message string) (string, string, map[string]interface{}, bool) {
	message = strings.TrimSpace(message)
	lowerMsg := strings.ToLower(message)

	// Check for anime model shortcut
	if strings.HasPrefix(lowerMsg, "anime:") {
		content := strings.TrimSpace(message[6:])
		params := map[string]interface{}{
			"model": "wai-Illustrious", // Anime model
			"steps": 30,                // wai-Illustrious max steps is 30
		}
		return "image", content, params, true
	}

	// Check for hidream model shortcut
	if strings.HasPrefix(lowerMsg, "dream:") {
		content := strings.TrimSpace(message[6:])
		params := map[string]interface{}{
			"model": "hidream", // High quality dream-like images
			"steps": 30,        // hidream max steps is 30
		}
		return "image", content, params, true
	}

	// Check for custom model syntax: image[model-name]: prompt
	if strings.HasPrefix(lowerMsg, "image[") {
		endBracket := strings.Index(lowerMsg, "]:")
		if endBracket > 6 {
			modelName := message[6:endBracket]
			content := strings.TrimSpace(message[endBracket+2:])
			params := map[string]interface{}{
				"model": modelName,
			}
			return "image", content, params, true
		}
	}

	// Check for standard media prefixes
	prefixes := map[string]string{
		"image:":   "image",
		"upscale:": "upscale",
	}

	for prefix, mediaType := range prefixes {
		if strings.HasPrefix(lowerMsg, prefix) {
			// Extract prompt/path after prefix
			content := strings.TrimSpace(message[len(prefix):])
			params := make(map[string]interface{})

			// For upscale, first word is file path
			if mediaType == "upscale" {
				parts := strings.SplitN(content, " ", 2)
				if len(parts) > 0 {
					params["path"] = parts[0]
					if len(parts) > 1 {
						content = parts[1] // Rest is additional params
					} else {
						content = ""
					}
				}
			}

			return mediaType, content, params, true
		}
	}

	return "", "", nil, false
}
