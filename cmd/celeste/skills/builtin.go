// Package skills provides the skill registry and execution system.
// This file contains built-in skill implementations.
package skills

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
)

// RegisterBuiltinSkills registers all built-in skills with the registry.
func RegisterBuiltinSkills(registry *Registry, configLoader ConfigLoader) {
	// Register skill definitions
	registry.RegisterSkill(TarotSkill())
	registry.RegisterSkill(WeatherSkill())
	registry.RegisterSkill(UnitConverterSkill())
	registry.RegisterSkill(TimezoneConverterSkill())
	registry.RegisterSkill(HashGeneratorSkill())
	registry.RegisterSkill(Base64EncodeSkill())
	registry.RegisterSkill(Base64DecodeSkill())
	registry.RegisterSkill(UUIDGeneratorSkill())
	registry.RegisterSkill(PasswordGeneratorSkill())
	registry.RegisterSkill(CurrencyConverterSkill())
	registry.RegisterSkill(QRCodeGeneratorSkill())
	registry.RegisterSkill(TwitchLiveCheckSkill())
	registry.RegisterSkill(YouTubeVideosSkill())
	registry.RegisterSkill(SetReminderSkill())
	registry.RegisterSkill(ListRemindersSkill())
	registry.RegisterSkill(SaveNoteSkill())
	registry.RegisterSkill(GetNoteSkill())
	registry.RegisterSkill(ListNotesSkill())

	// Register handlers
	registry.RegisterHandler("tarot_reading", func(args map[string]interface{}) (interface{}, error) {
		return TarotHandler(args, configLoader)
	})
	registry.RegisterHandler("get_weather", func(args map[string]interface{}) (interface{}, error) {
		return WeatherHandler(args, configLoader)
	})
	registry.RegisterHandler("convert_units", func(args map[string]interface{}) (interface{}, error) {
		return UnitConverterHandler(args)
	})
	registry.RegisterHandler("convert_timezone", func(args map[string]interface{}) (interface{}, error) {
		return TimezoneConverterHandler(args)
	})
	registry.RegisterHandler("generate_hash", func(args map[string]interface{}) (interface{}, error) {
		return HashGeneratorHandler(args)
	})
	registry.RegisterHandler("base64_encode", func(args map[string]interface{}) (interface{}, error) {
		return Base64EncodeHandler(args)
	})
	registry.RegisterHandler("base64_decode", func(args map[string]interface{}) (interface{}, error) {
		return Base64DecodeHandler(args)
	})
	registry.RegisterHandler("generate_uuid", func(args map[string]interface{}) (interface{}, error) {
		return UUIDGeneratorHandler(args)
	})
	registry.RegisterHandler("generate_password", func(args map[string]interface{}) (interface{}, error) {
		return PasswordGeneratorHandler(args)
	})
	registry.RegisterHandler("convert_currency", func(args map[string]interface{}) (interface{}, error) {
		return CurrencyConverterHandler(args)
	})
	registry.RegisterHandler("generate_qr_code", func(args map[string]interface{}) (interface{}, error) {
		return QRCodeGeneratorHandler(args)
	})
	registry.RegisterHandler("check_twitch_live", func(args map[string]interface{}) (interface{}, error) {
		return TwitchLiveCheckHandler(args, configLoader)
	})
	registry.RegisterHandler("get_youtube_videos", func(args map[string]interface{}) (interface{}, error) {
		return YouTubeVideosHandler(args, configLoader)
	})
	registry.RegisterHandler("set_reminder", func(args map[string]interface{}) (interface{}, error) {
		return SetReminderHandler(args)
	})
	registry.RegisterHandler("list_reminders", func(args map[string]interface{}) (interface{}, error) {
		return ListRemindersHandler(args)
	})
	registry.RegisterHandler("save_note", func(args map[string]interface{}) (interface{}, error) {
		return SaveNoteHandler(args)
	})
	registry.RegisterHandler("get_note", func(args map[string]interface{}) (interface{}, error) {
		return GetNoteHandler(args)
	})
	registry.RegisterHandler("list_notes", func(args map[string]interface{}) (interface{}, error) {
		return ListNotesHandler(args)
	})

	// Register crypto skills (IPFS, Alchemy, Blockchain Monitoring)
	RegisterCryptoSkills(registry, configLoader)
}

// ConfigLoader provides access to configuration values.
type ConfigLoader interface {
	GetTarotConfig() (TarotConfig, error)
	GetVeniceConfig() (VeniceConfig, error)
	GetWeatherConfig() (WeatherConfig, error)
	GetTwitchConfig() (TwitchConfig, error)
	GetYouTubeConfig() (YouTubeConfig, error)
	GetIPFSConfig() (IPFSConfig, error)
	GetAlchemyConfig() (AlchemyConfig, error)
	GetBlockmonConfig() (BlockmonConfig, error)
	GetWalletSecurityConfig() (WalletSecuritySettingsConfig, error)
}

// TarotConfig holds tarot function configuration.
type TarotConfig struct {
	FunctionURL string
	AuthToken   string
}

// VeniceConfig holds Venice.ai configuration.
type VeniceConfig struct {
	APIKey     string
	BaseURL    string
	Model      string // Chat model (venice-uncensored)
	ImageModel string // Image generation model (lustify-sdxl, animewan, hidream, wai-Illustrious)
	Upscaler   string
}

// WeatherConfig holds weather skill configuration.
type WeatherConfig struct {
	DefaultZipCode string
}

// TwitchConfig holds Twitch API configuration.
type TwitchConfig struct {
	ClientID        string
	ClientSecret    string
	DefaultStreamer string
}

// YouTubeConfig holds YouTube API configuration.
type YouTubeConfig struct {
	APIKey         string
	DefaultChannel string
}

// IPFSConfig holds IPFS configuration.
type IPFSConfig struct {
	Provider       string
	APIKey         string
	APISecret      string
	ProjectID      string
	GatewayURL     string
	TimeoutSeconds int
}

// AlchemyConfig holds Alchemy API configuration.
type AlchemyConfig struct {
	APIKey         string
	DefaultNetwork string
	TimeoutSeconds int
}

// BlockmonConfig holds blockchain monitoring configuration.
type BlockmonConfig struct {
	AlchemyAPIKey       string
	WebhookURL          string
	DefaultNetwork      string
	PollIntervalSeconds int
}

// WalletSecuritySettingsConfig holds wallet security settings.
type WalletSecuritySettingsConfig struct {
	Enabled      bool
	PollInterval int    // seconds
	AlertLevel   string // minimum severity to alert on
}

// --- Helper Functions for Error Handling ---

// formatErrorResponse creates a structured error response for LLM interpretation.
func formatErrorResponse(errorType, message, hint string, context map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"error":      true,
		"error_type": errorType,
		"message":    message,
	}
	if hint != "" {
		result["hint"] = hint
	}
	// Merge context into result
	for k, v := range context {
		result[k] = v
	}
	return result
}

// getUserOrDefault gets a value from args first, then falls back to config default.
// Returns: (value, found) - found is true if value was found (either from args or config)
func getUserOrDefault(args map[string]interface{}, key string, configGetter func() string) (string, bool) {
	// Check user-provided value first
	if val, ok := args[key].(string); ok && val != "" {
		return val, true
	}
	// Fall back to config default
	if configGetter != nil {
		defaultVal := configGetter()
		if defaultVal != "" {
			return defaultVal, true
		}
	}
	return "", false
}

// formatConfigError creates a structured error when both user value and config default are missing.
func formatConfigError(skillName, fieldName, configCommand string) map[string]interface{} {
	return formatErrorResponse(
		"config_error",
		fmt.Sprintf("%s is required for %s. Please provide %s in your request, or set a default using: %s", fieldName, skillName, fieldName, configCommand),
		fmt.Sprintf("You can ask the user for their %s or location", fieldName),
		map[string]interface{}{
			"skill":          skillName,
			"field":          fieldName,
			"config_command": configCommand,
			"info":           fmt.Sprintf("No default %s configured. User must provide %s in request.", fieldName, fieldName),
		},
	)
}

// getConfigWithFallback safely gets config, returning empty config if error occurs.
// This allows handlers to check for user-provided values even when config is missing.
// Note: Go doesn't support generics in this way, so we'll use specific functions for each config type.

// --- Skill Definitions ---

// TarotSkill returns the tarot reading skill definition.
func TarotSkill() Skill {
	return Skill{
		Name:        "tarot_reading",
		Description: "Generate a tarot card reading using either a three-card spread (past/present/future) or a celtic cross spread",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spread_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"three", "celtic"},
					"description": "Type of spread: 'three' for 3-card past/present/future, 'celtic' for 10-card celtic cross",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "Optional question to focus the reading on",
				},
			},
			"required": []string{"spread_type"},
		},
	}
}

// WeatherSkill returns the weather forecast skill definition.
func WeatherSkill() Skill {
	return Skill{
		Name:        "get_weather",
		Description: "Get current weather and forecast for a location. Uses default zip code if not specified. User can provide zip code in the prompt to override default.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"zip_code": map[string]interface{}{
					"type":        "string",
					"description": "Optional zip code (5 digits). If not provided, uses default zip code from configuration. User can specify zip code in their message to override default.",
				},
				"days": map[string]interface{}{
					"type":        "integer",
					"description": "Number of days for forecast (1-3, default: 1 for current weather)",
				},
			},
			"required": []string{},
		},
	}
}

// UnitConverterSkill returns the unit converter skill definition.
func UnitConverterSkill() Skill {
	return Skill{
		Name:        "convert_units",
		Description: "Convert between different units of measurement (length, weight, temperature, volume)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"value": map[string]interface{}{
					"type":        "number",
					"description": "The numeric value to convert",
				},
				"from_unit": map[string]interface{}{
					"type":        "string",
					"description": "Source unit (e.g., 'm', 'km', 'ft', 'mi', 'kg', 'lb', 'celsius', 'fahrenheit', 'liter', 'gallon')",
				},
				"to_unit": map[string]interface{}{
					"type":        "string",
					"description": "Target unit (e.g., 'm', 'km', 'ft', 'mi', 'kg', 'lb', 'celsius', 'fahrenheit', 'liter', 'gallon')",
				},
			},
			"required": []string{"value", "from_unit", "to_unit"},
		},
	}
}

// TimezoneConverterSkill returns the timezone converter skill definition.
func TimezoneConverterSkill() Skill {
	return Skill{
		Name:        "convert_timezone",
		Description: "Convert time between different timezones",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"time": map[string]interface{}{
					"type":        "string",
					"description": "Time to convert (format: 'HH:MM' or 'HH:MM:SS', defaults to current time if not provided)",
				},
				"from_timezone": map[string]interface{}{
					"type":        "string",
					"description": "Source timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo')",
				},
				"to_timezone": map[string]interface{}{
					"type":        "string",
					"description": "Target timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo')",
				},
				"date": map[string]interface{}{
					"type":        "string",
					"description": "Optional date (format: 'YYYY-MM-DD', defaults to today if not provided)",
				},
			},
			"required": []string{"from_timezone", "to_timezone"},
		},
	}
}

// HashGeneratorSkill returns the hash generator skill definition.
func HashGeneratorSkill() Skill {
	return Skill{
		Name:        "generate_hash",
		Description: "Generate cryptographic hash (MD5, SHA256, SHA512) for a given string",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to hash",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"md5", "sha256", "sha512"},
					"description": "Hash algorithm to use",
				},
			},
			"required": []string{"text", "algorithm"},
		},
	}
}

// Base64EncodeSkill returns the base64 encoder skill definition.
func Base64EncodeSkill() Skill {
	return Skill{
		Name:        "base64_encode",
		Description: "Encode a string to base64",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to encode",
				},
			},
			"required": []string{"text"},
		},
	}
}

// Base64DecodeSkill returns the base64 decoder skill definition.
func Base64DecodeSkill() Skill {
	return Skill{
		Name:        "base64_decode",
		Description: "Decode a base64 string",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"encoded": map[string]interface{}{
					"type":        "string",
					"description": "Base64 encoded string to decode",
				},
			},
			"required": []string{"encoded"},
		},
	}
}

// UUIDGeneratorSkill returns the UUID generator skill definition.
func UUIDGeneratorSkill() Skill {
	return Skill{
		Name:        "generate_uuid",
		Description: "Generate a random UUID (v4)",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}
}

// PasswordGeneratorSkill returns the password generator skill definition.
func PasswordGeneratorSkill() Skill {
	return Skill{
		Name:        "generate_password",
		Description: "Generate a secure random password",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"length": map[string]interface{}{
					"type":        "integer",
					"description": "Password length (default: 16, min: 8, max: 128)",
				},
				"include_symbols": map[string]interface{}{
					"type":        "boolean",
					"description": "Include special symbols (default: true)",
				},
				"include_numbers": map[string]interface{}{
					"type":        "boolean",
					"description": "Include numbers (default: true)",
				},
			},
			"required": []string{},
		},
	}
}

// CurrencyConverterSkill returns the currency converter skill definition.
func CurrencyConverterSkill() Skill {
	return Skill{
		Name:        "convert_currency",
		Description: "Convert between different currencies using current exchange rates",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"amount": map[string]interface{}{
					"type":        "number",
					"description": "Amount to convert",
				},
				"from_currency": map[string]interface{}{
					"type":        "string",
					"description": "Source currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP')",
				},
				"to_currency": map[string]interface{}{
					"type":        "string",
					"description": "Target currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP')",
				},
			},
			"required": []string{"amount", "from_currency", "to_currency"},
		},
	}
}

// QRCodeGeneratorSkill returns the QR code generator skill definition.
func QRCodeGeneratorSkill() Skill {
	return Skill{
		Name:        "generate_qr_code",
		Description: "Generate a QR code from text or URL",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text or URL to encode in QR code",
				},
				"size": map[string]interface{}{
					"type":        "integer",
					"description": "QR code size in pixels (default: 256, min: 64, max: 1024)",
				},
			},
			"required": []string{"text"},
		},
	}
}

// TwitchLiveCheckSkill returns the Twitch live check skill definition.
func TwitchLiveCheckSkill() Skill {
	return Skill{
		Name:        "check_twitch_live",
		Description: "Check if a Twitch streamer is currently live. Uses default streamer if not specified. User can provide streamer name in prompt to override default.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"streamer": map[string]interface{}{
					"type":        "string",
					"description": "Optional Twitch streamer username. If not provided, uses default streamer from configuration. User can specify streamer name in their message to override default.",
				},
			},
			"required": []string{},
		},
	}
}

// YouTubeVideosSkill returns the YouTube recent videos skill definition.
func YouTubeVideosSkill() Skill {
	return Skill{
		Name:        "get_youtube_videos",
		Description: "Get recent videos from a YouTube channel. Uses default channel if not specified. User can provide channel name/ID in prompt to override default.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{
					"type":        "string",
					"description": "Optional YouTube channel username or channel ID. If not provided, uses default channel from configuration. User can specify channel in their message to override default.",
				},
				"max_results": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of videos to return (default: 5, min: 1, max: 50)",
				},
			},
			"required": []string{},
		},
	}
}

// SetReminderSkill returns the set reminder skill definition.
func SetReminderSkill() Skill {
	return Skill{
		Name:        "set_reminder",
		Description: "Set a reminder with a specific time and message",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Reminder message",
				},
				"time": map[string]interface{}{
					"type":        "string",
					"description": "Time for reminder (format: 'YYYY-MM-DD HH:MM' or 'HH:MM' for today, or relative like 'in 1 hour', 'tomorrow at 3pm')",
				},
			},
			"required": []string{"message", "time"},
		},
	}
}

// ListRemindersSkill returns the list reminders skill definition.
func ListRemindersSkill() Skill {
	return Skill{
		Name:        "list_reminders",
		Description: "List all active reminders",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}
}

// SaveNoteSkill returns the save note skill definition.
func SaveNoteSkill() Skill {
	return Skill{
		Name:        "save_note",
		Description: "Save a note with an optional title",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Optional note title",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Note content",
				},
			},
			"required": []string{"content"},
		},
	}
}

// GetNoteSkill returns the get note skill definition.
func GetNoteSkill() Skill {
	return Skill{
		Name:        "get_note",
		Description: "Retrieve a note by title",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Note title to retrieve",
				},
			},
			"required": []string{"title"},
		},
	}
}

// ListNotesSkill returns the list notes skill definition.
func ListNotesSkill() Skill {
	return Skill{
		Name:        "list_notes",
		Description: "List all saved notes",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}
}

// --- Skill Handlers ---

// TarotHandler executes a tarot reading.
func TarotHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	config, err := configLoader.GetTarotConfig()
	if err != nil {
		return formatErrorResponse(
			"config_error",
			"Tarot configuration is required. Please configure it using: celeste config --set-tarot-token <token>",
			"The tarot auth token is needed to access the tarot reading service.",
			map[string]interface{}{
				"skill":          "tarot_reading",
				"config_command": "celeste config --set-tarot-token <token>",
			},
		), nil
	}

	spreadType := "three"
	if st, ok := args["spread_type"].(string); ok {
		spreadType = st
	}

	question := ""
	if q, ok := args["question"].(string); ok {
		question = q
	}

	// Make request to tarot function
	requestBody := map[string]interface{}{
		"spread_type": spreadType,
	}
	if question != "" {
		requestBody["question"] = question
	}

	reqBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to encode tarot request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "tarot_reading",
				"error": err.Error(),
			},
		), nil
	}

	req, err := http.NewRequest("POST", config.FunctionURL, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to create tarot request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "tarot_reading",
				"error": err.Error(),
			},
		), nil
	}

	// Set Authorization header - token may already include "Basic " prefix
	authToken := config.AuthToken
	if !strings.HasPrefix(authToken, "Basic ") {
		authToken = "Basic " + authToken
	}
	req.Header.Set("Authorization", authToken)
	req.Header.Set("Content-Type", "application/json")

	// Create client with timeout and logging
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make request with timeout
	startTime := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(startTime)

	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to connect to tarot API",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		), nil
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to read tarot API response",
			"The tarot service may have returned invalid data. Please try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		), nil
	}

	if resp.StatusCode != 200 {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Tarot API returned error (status %d)", resp.StatusCode),
			"The tarot reading service may be temporarily unavailable. Please try again later.",
			map[string]interface{}{
				"skill":       "tarot_reading",
				"status_code": resp.StatusCode,
				"response":    string(responseBody),
				"elapsed":     elapsed.String(),
			},
		), nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse tarot API response",
			"The tarot service returned invalid data. Please try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		), nil
	}

	return result, nil
}

// WeatherHandler gets weather forecast for a location.
func WeatherHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Try to get config, but don't fail if it's not configured
	config, err := configLoader.GetWeatherConfig()
	if err != nil {
		// If config not available, use empty config (will require zip in args)
		config = WeatherConfig{}
	}

	// Get zip code - accept both string and number types
	var zipCode string
	var found bool

	// Try string first
	if val, ok := args["zip_code"].(string); ok && val != "" {
		zipCode = val
		found = true
	} else if val, ok := args["zip_code"].(float64); ok {
		// Convert number to string (for CLI numeric conversion)
		zipCode = fmt.Sprintf("%.0f", val)
		found = true
	} else {
		// Fall back to config default
		zipCode = config.DefaultZipCode
		found = zipCode != ""
	}

	// Only return error/info if BOTH user-provided zip AND default are missing
	if !found {
		return formatConfigError("get_weather", "zip_code", "celeste config --set-weather-zip <zip>"), nil
	}

	// Validate zip code format (5 digits)
	if len(zipCode) != 5 {
		return formatErrorResponse(
			"validation_error",
			"Zip code must be exactly 5 digits",
			"Please provide a valid 5-digit US zip code",
			map[string]interface{}{
				"skill":    "get_weather",
				"field":    "zip_code",
				"provided": zipCode,
			},
		), nil
	}
	for _, c := range zipCode {
		if c < '0' || c > '9' {
			return formatErrorResponse(
				"validation_error",
				"Zip code must contain only digits",
				"Please provide a valid 5-digit US zip code",
				map[string]interface{}{
					"skill":    "get_weather",
					"field":    "zip_code",
					"provided": zipCode,
				},
			), nil
		}
	}

	// Get forecast days (default 1 for current weather)
	days := 1
	if d, ok := args["days"].(float64); ok {
		days = int(d)
		if days < 1 {
			days = 1
		}
		if days > 3 {
			days = 3
		}
	}

	// Use wttr.in API (free, no key required)
	// Format: https://wttr.in/{zip}?format=j1 for JSON
	url := fmt.Sprintf("https://wttr.in/%s?format=j1", zipCode)
	if days > 1 {
		url = fmt.Sprintf("https://wttr.in/%s?format=j1&days=%d", zipCode, days)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to connect to weather service",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill": "get_weather",
				"error": err.Error(),
			},
		), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Weather API returned error (status %d)", resp.StatusCode),
			"The weather service may be temporarily unavailable. Please try again later.",
			map[string]interface{}{
				"skill":       "get_weather",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		), nil
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse weather API response",
			"The weather service returned invalid data. Please try again.",
			map[string]interface{}{
				"skill": "get_weather",
				"error": err.Error(),
			},
		), nil
	}

	// Add zip code to result for reference
	result["zip_code"] = zipCode
	result["requested_days"] = days

	return result, nil
}

// UnitConverterHandler converts between different units.
func UnitConverterHandler(args map[string]interface{}) (interface{}, error) {
	value, ok := args["value"].(float64)
	if !ok {
		return formatErrorResponse(
			"validation_error",
			"The 'value' parameter must be a number",
			"Please provide a numeric value to convert.",
			map[string]interface{}{
				"skill": "convert_units",
				"field": "value",
			},
		), nil
	}

	fromUnit, ok := args["from_unit"].(string)
	if !ok || fromUnit == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'from_unit' parameter is required",
			"Please specify the source unit (e.g., 'm', 'km', 'ft', 'kg', 'lb', 'celsius', 'fahrenheit').",
			map[string]interface{}{
				"skill": "convert_units",
				"field": "from_unit",
			},
		), nil
	}

	toUnit, ok := args["to_unit"].(string)
	if !ok || toUnit == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'to_unit' parameter is required",
			"Please specify the target unit (e.g., 'm', 'km', 'ft', 'kg', 'lb', 'celsius', 'fahrenheit').",
			map[string]interface{}{
				"skill": "convert_units",
				"field": "to_unit",
			},
		), nil
	}

	// Convert to lowercase for comparison
	fromUnit = strings.ToLower(fromUnit)
	toUnit = strings.ToLower(toUnit)

	// Length conversions (meters as base)
	lengthConversions := map[string]float64{
		"m":  1.0,
		"km": 1000.0,
		"cm": 0.01,
		"mm": 0.001,
		"ft": 0.3048,
		"in": 0.0254,
		"yd": 0.9144,
		"mi": 1609.34,
	}

	// Weight conversions (kilograms as base)
	weightConversions := map[string]float64{
		"kg": 1.0,
		"g":  0.001,
		"mg": 0.000001,
		"lb": 0.453592,
		"oz": 0.0283495,
	}

	// Volume conversions (liters as base)
	volumeConversions := map[string]float64{
		"l":      1.0,
		"liter":  1.0,
		"ml":     0.001,
		"gallon": 3.78541,
		"quart":  0.946353,
		"pint":   0.473176,
		"cup":    0.236588,
		"fl oz":  0.0295735,
	}

	var result float64
	var category string

	// Check if it's a length conversion
	if fromVal, fromOk := lengthConversions[fromUnit]; fromOk {
		if toVal, toOk := lengthConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "length"
		} else {
			return formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for length conversion", toUnit),
				"Valid length units: m, km, cm, mm, ft, in, yd, mi",
				map[string]interface{}{
					"skill":    "convert_units",
					"field":    "to_unit",
					"provided": toUnit,
					"category": "length",
				},
			), nil
		}
	} else if fromVal, fromOk := weightConversions[fromUnit]; fromOk {
		// Check if it's a weight conversion
		if toVal, toOk := weightConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "weight"
		} else {
			return formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for weight conversion", toUnit),
				"Valid weight units: kg, g, mg, lb, oz",
				map[string]interface{}{
					"skill":    "convert_units",
					"field":    "to_unit",
					"provided": toUnit,
					"category": "weight",
				},
			), nil
		}
	} else if fromVal, fromOk := volumeConversions[fromUnit]; fromOk {
		// Check if it's a volume conversion
		if toVal, toOk := volumeConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "volume"
		} else {
			return formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for volume conversion", toUnit),
				"Valid volume units: l, liter, ml, gallon, quart, pint, cup, fl oz",
				map[string]interface{}{
					"skill":    "convert_units",
					"field":    "to_unit",
					"provided": toUnit,
					"category": "volume",
				},
			), nil
		}
	} else if strings.Contains(fromUnit, "celsius") || strings.Contains(fromUnit, "fahrenheit") {
		// Temperature conversion
		category = "temperature"
		var celsius float64

		if strings.Contains(fromUnit, "fahrenheit") {
			celsius = (value - 32) * 5 / 9
		} else {
			celsius = value
		}

		if strings.Contains(toUnit, "fahrenheit") {
			result = celsius*9/5 + 32
		} else {
			result = celsius
		}
	} else {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unsupported unit conversion from '%s' to '%s'", fromUnit, toUnit),
			"Please ensure both units are of the same type (length, weight, temperature, or volume).",
			map[string]interface{}{
				"skill":     "convert_units",
				"from_unit": fromUnit,
				"to_unit":   toUnit,
			},
		), nil
	}

	return map[string]interface{}{
		"value":      result,
		"from_value": value,
		"from_unit":  fromUnit,
		"to_unit":    toUnit,
		"category":   category,
	}, nil
}

// TimezoneConverterHandler converts time between timezones.
func TimezoneConverterHandler(args map[string]interface{}) (interface{}, error) {
	fromTZ, ok := args["from_timezone"].(string)
	if !ok || fromTZ == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'from_timezone' parameter is required",
			"Please specify the source timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]interface{}{
				"skill": "convert_timezone",
				"field": "from_timezone",
			},
		), nil
	}

	toTZ, ok := args["to_timezone"].(string)
	if !ok || toTZ == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'to_timezone' parameter is required",
			"Please specify the target timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]interface{}{
				"skill": "convert_timezone",
				"field": "to_timezone",
			},
		), nil
	}

	// Load timezone locations
	fromLoc, err := time.LoadLocation(fromTZ)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid timezone '%s'", fromTZ),
			"Please use a valid IANA timezone identifier (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]interface{}{
				"skill":    "convert_timezone",
				"field":    "from_timezone",
				"provided": fromTZ,
				"error":    err.Error(),
			},
		), nil
	}

	toLoc, err := time.LoadLocation(toTZ)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid timezone '%s'", toTZ),
			"Please use a valid IANA timezone identifier (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]interface{}{
				"skill":    "convert_timezone",
				"field":    "to_timezone",
				"provided": toTZ,
				"error":    err.Error(),
			},
		), nil
	}

	// Parse time if provided, otherwise use current time
	var t time.Time
	if timeStr, ok := args["time"].(string); ok && timeStr != "" {
		// Parse time string (HH:MM or HH:MM:SS)
		var dateStr string
		if date, ok := args["date"].(string); ok && date != "" {
			dateStr = date
		} else {
			dateStr = time.Now().In(fromLoc).Format("2006-01-02")
		}

		timeLayout := "2006-01-02 15:04:05"
		if !strings.Contains(timeStr, ":") {
			return formatErrorResponse(
				"validation_error",
				"Invalid time format",
				"Please use format 'HH:MM' or 'HH:MM:SS' (e.g., '14:30' or '14:30:00').",
				map[string]interface{}{
					"skill":    "convert_timezone",
					"field":    "time",
					"provided": timeStr,
				},
			), nil
		}
		if len(strings.Split(timeStr, ":")[0]) == 1 {
			timeStr = "0" + timeStr
		}
		if len(strings.Split(timeStr, ":")) == 2 {
			timeStr = timeStr + ":00"
		}

		fullTimeStr := dateStr + " " + timeStr
		t, err = time.ParseInLocation(timeLayout, fullTimeStr, fromLoc)
		if err != nil {
			return formatErrorResponse(
				"validation_error",
				"Invalid time format",
				"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
				map[string]interface{}{
					"skill":    "convert_timezone",
					"field":    "time",
					"provided": timeStr,
					"error":    err.Error(),
				},
			), nil
		}
	} else {
		// Use current time in source timezone
		if date, ok := args["date"].(string); ok && date != "" {
			dateStr := date + " 00:00:00"
			timeLayout := "2006-01-02 15:04:05"
			t, err = time.ParseInLocation(timeLayout, dateStr, fromLoc)
			if err != nil {
				return formatErrorResponse(
					"validation_error",
					"Invalid date format",
					"Please use format 'YYYY-MM-DD' (e.g., '2024-12-03').",
					map[string]interface{}{
						"skill":    "convert_timezone",
						"field":    "date",
						"provided": date,
						"error":    err.Error(),
					},
				), nil
			}
		} else {
			t = time.Now().In(fromLoc)
		}
	}

	// Convert to target timezone
	converted := t.In(toLoc)

	return map[string]interface{}{
		"original_time":   t.Format("2006-01-02 15:04:05 MST"),
		"converted_time":  converted.Format("2006-01-02 15:04:05 MST"),
		"from_timezone":   fromTZ,
		"to_timezone":     toTZ,
		"original_utc":    t.UTC().Format("2006-01-02 15:04:05 UTC"),
		"converted_utc":   converted.UTC().Format("2006-01-02 15:04:05 UTC"),
		"timezone_offset": converted.Format("-07:00"),
	}, nil
}

// HashGeneratorHandler generates a hash for the given text.
func HashGeneratorHandler(args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text you want to hash.",
			map[string]interface{}{
				"skill": "generate_hash",
				"field": "text",
			},
		), nil
	}

	algorithm, ok := args["algorithm"].(string)
	if !ok || algorithm == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'algorithm' parameter is required",
			"Please specify a hash algorithm: 'md5', 'sha256', or 'sha512'.",
			map[string]interface{}{
				"skill": "generate_hash",
				"field": "algorithm",
			},
		), nil
	}

	algorithm = strings.ToLower(algorithm)
	var hash string

	switch algorithm {
	case "md5":
		h := md5.Sum([]byte(text))
		hash = hex.EncodeToString(h[:])
	case "sha256":
		h := sha256.Sum256([]byte(text))
		hash = hex.EncodeToString(h[:])
	case "sha512":
		h := sha512.Sum512([]byte(text))
		hash = hex.EncodeToString(h[:])
	default:
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unsupported algorithm '%s'", algorithm),
			"Please use one of: 'md5', 'sha256', or 'sha512'.",
			map[string]interface{}{
				"skill":     "generate_hash",
				"field":     "algorithm",
				"provided":  algorithm,
				"supported": []string{"md5", "sha256", "sha512"},
			},
		), nil
	}

	return map[string]interface{}{
		"text":      text,
		"algorithm": algorithm,
		"hash":      hash,
	}, nil
}

// Base64EncodeHandler encodes text to base64.
func Base64EncodeHandler(args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text you want to encode.",
			map[string]interface{}{
				"skill": "base64_encode",
				"field": "text",
			},
		), nil
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(text))

	return map[string]interface{}{
		"original": text,
		"encoded":  encoded,
	}, nil
}

// Base64DecodeHandler decodes a base64 string.
func Base64DecodeHandler(args map[string]interface{}) (interface{}, error) {
	encoded, ok := args["encoded"].(string)
	if !ok || encoded == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'encoded' parameter is required",
			"Please provide the base64 encoded string you want to decode.",
			map[string]interface{}{
				"skill": "base64_decode",
				"field": "encoded",
			},
		), nil
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			"Invalid base64 string",
			"The provided string is not valid base64 encoded data.",
			map[string]interface{}{
				"skill": "base64_decode",
				"field": "encoded",
				"error": err.Error(),
			},
		), nil
	}

	return map[string]interface{}{
		"encoded": encoded,
		"decoded": string(decoded),
	}, nil
}

// UUIDGeneratorHandler generates a UUID v4.
func UUIDGeneratorHandler(args map[string]interface{}) (interface{}, error) {
	id := uuid.New()

	return map[string]interface{}{
		"uuid": id.String(),
	}, nil
}

// PasswordGeneratorHandler generates a secure random password.
func PasswordGeneratorHandler(args map[string]interface{}) (interface{}, error) {
	length := 16
	if l, ok := args["length"].(float64); ok {
		length = int(l)
		if length < 8 {
			length = 8
		}
		if length > 128 {
			length = 128
		}
	}

	includeSymbols := true
	if s, ok := args["include_symbols"].(bool); ok {
		includeSymbols = s
	}

	includeNumbers := true
	if n, ok := args["include_numbers"].(bool); ok {
		includeNumbers = n
	}

	// Character sets
	lowercase := "abcdefghijklmnopqrstuvwxyz"
	uppercase := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	numbers := "0123456789"
	symbols := "!@#$%^&*()_+-=[]{}|;:,.<>?"

	charset := lowercase + uppercase
	if includeNumbers {
		charset += numbers
	}
	if includeSymbols {
		charset += symbols
	}

	password := make([]byte, length)
	for i := range password {
		b := make([]byte, 1)
		// rand.Read on crypto/rand never fails for fixed-size buffers
		_, _ = rand.Read(b)
		password[i] = charset[int(b[0])%len(charset)]
	}

	return map[string]interface{}{
		"password":        string(password),
		"length":          length,
		"include_symbols": includeSymbols,
		"include_numbers": includeNumbers,
	}, nil
}

// CurrencyConverterHandler converts between currencies using exchangerate-api.com.
func CurrencyConverterHandler(args map[string]interface{}) (interface{}, error) {
	amount, ok := args["amount"].(float64)
	if !ok {
		return formatErrorResponse(
			"validation_error",
			"The 'amount' parameter must be a number",
			"Please provide a numeric amount to convert.",
			map[string]interface{}{
				"skill": "convert_currency",
				"field": "amount",
			},
		), nil
	}

	fromCurrency, ok := args["from_currency"].(string)
	if !ok || fromCurrency == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'from_currency' parameter is required",
			"Please specify the source currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP').",
			map[string]interface{}{
				"skill": "convert_currency",
				"field": "from_currency",
			},
		), nil
	}

	toCurrency, ok := args["to_currency"].(string)
	if !ok || toCurrency == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'to_currency' parameter is required",
			"Please specify the target currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP').",
			map[string]interface{}{
				"skill": "convert_currency",
				"field": "to_currency",
			},
		), nil
	}

	// Normalize currency codes to uppercase
	fromCurrency = strings.ToUpper(fromCurrency)
	toCurrency = strings.ToUpper(toCurrency)

	if fromCurrency == toCurrency {
		return map[string]interface{}{
			"amount":        amount,
			"from_currency": fromCurrency,
			"to_currency":   toCurrency,
			"converted":     amount,
			"rate":          1.0,
		}, nil
	}

	// Use exchangerate-api.com free tier (no key required for basic)
	// First get rates for the base currency
	url := fmt.Sprintf("https://api.exchangerate-api.com/v6/latest/%s", fromCurrency)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to connect to currency API",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill": "convert_currency",
				"error": err.Error(),
			},
		), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Currency API returned error (status %d)", resp.StatusCode),
			"The currency exchange service may be temporarily unavailable. Please try again later.",
			map[string]interface{}{
				"skill":       "convert_currency",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		), nil
	}

	var result struct {
		Rates map[string]float64 `json:"rates"`
		Base  string             `json:"base"`
		Date  string             `json:"date"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse currency API response",
			"The currency service returned invalid data. Please try again.",
			map[string]interface{}{
				"skill": "convert_currency",
				"error": err.Error(),
			},
		), nil
	}

	rate, ok := result.Rates[toCurrency]
	if !ok {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Currency %s not found in exchange rates", toCurrency),
			"Please use a valid 3-letter currency code (e.g., USD, EUR, JPY, GBP)",
			map[string]interface{}{
				"skill":    "convert_currency",
				"field":    "to_currency",
				"provided": toCurrency,
			},
		), nil
	}

	converted := amount * rate

	return map[string]interface{}{
		"amount":        amount,
		"from_currency": fromCurrency,
		"to_currency":   toCurrency,
		"converted":     converted,
		"rate":          rate,
		"date":          result.Date,
	}, nil
}

// QRCodeGeneratorHandler generates a QR code from text.
func QRCodeGeneratorHandler(args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text or URL you want to encode in the QR code.",
			map[string]interface{}{
				"skill": "generate_qr_code",
				"field": "text",
			},
		), nil
	}

	size := 256
	if s, ok := args["size"].(float64); ok {
		size = int(s)
		if size < 64 {
			size = 64
		}
		if size > 1024 {
			size = 1024
		}
	}

	// Generate QR code
	qr, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to generate QR code",
			"An internal error occurred while generating the QR code. Please try again.",
			map[string]interface{}{
				"skill": "generate_qr_code",
				"error": err.Error(),
			},
		), nil
	}

	// Get QR code as PNG bytes
	pngData, err := qr.PNG(size)
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to encode QR code as PNG",
			"An internal error occurred while encoding the QR code. Please try again.",
			map[string]interface{}{
				"skill": "generate_qr_code",
				"error": err.Error(),
			},
		), nil
	}

	// Save to file
	homeDir, _ := os.UserHomeDir()
	qrDir := filepath.Join(homeDir, ".celeste", "qr_codes")
	os.MkdirAll(qrDir, 0755)

	filename := fmt.Sprintf("qr_%d.png", time.Now().Unix())
	filepath := filepath.Join(qrDir, filename)

	if err := os.WriteFile(filepath, pngData, 0644); err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to save QR code file",
			"An internal error occurred while saving the QR code. Please try again.",
			map[string]interface{}{
				"skill":    "generate_qr_code",
				"error":    err.Error(),
				"filepath": filepath,
			},
		), nil
	}

	return map[string]interface{}{
		"text":     text,
		"size":     size,
		"filepath": filepath,
		"success":  true,
	}, nil
}

// TwitchLiveCheckHandler checks if a Twitch streamer is live.
func TwitchLiveCheckHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Try to get config, but don't fail if it's not configured
	config, err := configLoader.GetTwitchConfig()
	if err != nil {
		// If config not available, use empty config (will require streamer in args)
		config = TwitchConfig{}
	}

	// Get streamer using unified helper: user-provided first, then default
	streamer, found := getUserOrDefault(args, "streamer", func() string {
		return config.DefaultStreamer
	})

	// Only return error if BOTH user-provided streamer AND default are missing
	if !found {
		return formatConfigError("check_twitch_live", "streamer", "celeste config --set-twitch-streamer <name>"), nil
	}

	// Check if Client ID and Secret are configured (required for OAuth)
	if config.ClientID == "" || config.ClientSecret == "" {
		return formatErrorResponse(
			"config_error",
			"Twitch Client ID and Secret are required. Please configure them in skills.json.",
			"The Twitch API requires OAuth authentication. You need both Client ID and Client Secret from the Twitch Developer Console.",
			map[string]interface{}{
				"skill":          "check_twitch_live",
				"config_command": "Add twitch_client_id and twitch_client_secret to ~/.celeste/skills.json",
			},
		), nil
	}

	// Step 1: Get OAuth token using Client Credentials flow
	tokenURL := "https://id.twitch.tv/oauth2/token"
	tokenData := fmt.Sprintf("client_id=%s&client_secret=%s&grant_type=client_credentials",
		config.ClientID, config.ClientSecret)

	client := &http.Client{Timeout: 10 * time.Second}
	tokenReq, err := http.NewRequest("POST", tokenURL, strings.NewReader(tokenData))
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to create OAuth request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to get Twitch OAuth token",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != 200 {
		body, _ := io.ReadAll(tokenResp.Body)
		return formatErrorResponse(
			"auth_error",
			"Failed to authenticate with Twitch",
			"The Twitch Client ID or Secret may be invalid. Please check your configuration.",
			map[string]interface{}{
				"skill":       "check_twitch_live",
				"status_code": tokenResp.StatusCode,
				"response":    string(body),
			},
		), nil
	}

	var tokenResult struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse OAuth token response",
			"The Twitch OAuth API returned invalid data. Please try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}

	// Step 2: Use OAuth token to check if streamer is live
	url := fmt.Sprintf("https://api.twitch.tv/helix/streams?user_login=%s", streamer)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to create Twitch API request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}

	req.Header.Set("Client-ID", config.ClientID)
	req.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to connect to Twitch API",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Twitch API returned error (status %d)", resp.StatusCode),
			"The Twitch API may be temporarily unavailable or the streamer may not exist.",
			map[string]interface{}{
				"skill":       "check_twitch_live",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		), nil
	}

	var result struct {
		Data []struct {
			ID           string    `json:"id"`
			UserID       string    `json:"user_id"`
			UserLogin    string    `json:"user_login"`
			UserName     string    `json:"user_name"`
			GameID       string    `json:"game_id"`
			GameName     string    `json:"game_name"`
			Type         string    `json:"type"`
			Title        string    `json:"title"`
			ViewerCount  int       `json:"viewer_count"`
			StartedAt    time.Time `json:"started_at"`
			Language     string    `json:"language"`
			ThumbnailURL string    `json:"thumbnail_url"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse Twitch API response",
			"The Twitch API returned invalid data. Please try again.",
			map[string]interface{}{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		), nil
	}

	isLive := len(result.Data) > 0

	response := map[string]interface{}{
		"streamer": streamer,
		"is_live":  isLive,
	}

	if isLive {
		stream := result.Data[0]
		response["title"] = stream.Title
		response["game"] = stream.GameName
		response["viewer_count"] = stream.ViewerCount
		response["started_at"] = stream.StartedAt.Format(time.RFC3339)
		response["language"] = stream.Language
		response["thumbnail_url"] = stream.ThumbnailURL
		response["stream_url"] = fmt.Sprintf("https://www.twitch.tv/%s", stream.UserLogin)
	}

	return response, nil
}

// YouTubeVideosHandler gets recent videos from a YouTube channel.
func YouTubeVideosHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Try to get config, but don't fail if it's not configured
	config, err := configLoader.GetYouTubeConfig()
	if err != nil {
		// If config not available, use empty config (will require channel in args)
		config = YouTubeConfig{}
	}

	// Get channel using unified helper: user-provided first, then default
	channel, found := getUserOrDefault(args, "channel", func() string {
		return config.DefaultChannel
	})

	// Only return error if BOTH user-provided channel AND default are missing
	if !found {
		return formatConfigError("get_youtube_videos", "channel", "celeste config --set-youtube-channel <name>"), nil
	}

	// Check if API key is configured (required for API call)
	if config.APIKey == "" {
		return formatErrorResponse(
			"config_error",
			"YouTube API key is required. Please configure it using: celeste config --set-youtube-key <api-key>",
			"The YouTube API key is needed to access the YouTube Data API. You can get one from the Google Cloud Console.",
			map[string]interface{}{
				"skill":          "get_youtube_videos",
				"config_command": "celeste config --set-youtube-key <api-key>",
			},
		), nil
	}

	maxResults := 5
	if m, ok := args["max_results"].(float64); ok {
		maxResults = int(m)
		if maxResults < 1 {
			maxResults = 1
		}
		if maxResults > 50 {
			maxResults = 50
		}
	}

	// First, try to get channel ID if channel is a username
	// YouTube Data API v3 requires channel ID for search
	// We'll try to search by username first, then use channel ID
	channelID := channel

	// If it doesn't look like a channel ID (starts with UC), try to resolve it
	if !strings.HasPrefix(channel, "UC") && len(channel) != 24 {
		// Try to get channel ID from username
		searchURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/search?part=snippet&q=%s&type=channel&maxResults=1&key=%s", channel, config.APIKey)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(searchURL)
		if err == nil && resp.StatusCode == 200 {
			var searchResult struct {
				Items []struct {
					ID struct {
						ChannelID string `json:"channelId"`
					} `json:"id"`
				} `json:"items"`
			}
			if json.NewDecoder(resp.Body).Decode(&searchResult) == nil && len(searchResult.Items) > 0 {
				channelID = searchResult.Items[0].ID.ChannelID
			}
			resp.Body.Close()
		}
	}

	// Get recent videos
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/search?part=snippet&channelId=%s&order=date&type=video&maxResults=%d&key=%s", channelID, maxResults, config.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return formatErrorResponse(
			"network_error",
			"Failed to connect to YouTube API",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill": "get_youtube_videos",
				"error": err.Error(),
			},
		), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("YouTube API returned error (status %d)", resp.StatusCode),
			"The YouTube API may be temporarily unavailable or the channel may not exist.",
			map[string]interface{}{
				"skill":       "get_youtube_videos",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		), nil
	}

	var result struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title       string    `json:"title"`
				Description string    `json:"description"`
				PublishedAt time.Time `json:"publishedAt"`
				Thumbnails  struct {
					Default struct {
						URL string `json:"url"`
					} `json:"default"`
				} `json:"thumbnails"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return formatErrorResponse(
			"api_error",
			"Failed to parse YouTube API response",
			"The YouTube API returned invalid data. Please try again.",
			map[string]interface{}{
				"skill": "get_youtube_videos",
				"error": err.Error(),
			},
		), nil
	}

	videos := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		videos = append(videos, map[string]interface{}{
			"video_id":      item.ID.VideoID,
			"title":         item.Snippet.Title,
			"description":   item.Snippet.Description,
			"published_at":  item.Snippet.PublishedAt.Format(time.RFC3339),
			"thumbnail_url": item.Snippet.Thumbnails.Default.URL,
			"channel_title": item.Snippet.ChannelTitle,
			"url":           fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID.VideoID),
		})
	}

	return map[string]interface{}{
		"channel":    channel,
		"channel_id": channelID,
		"count":      len(videos),
		"videos":     videos,
	}, nil
}

// Reminder represents a reminder entry.
type Reminder struct {
	ID      string    `json:"id"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
	Created time.Time `json:"created"`
}

// Note represents a note entry.
type Note struct {
	Title   string    `json:"title"`
	Content string    `json:"content"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// getRemindersPath returns the path to reminders.json.
func getRemindersPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "reminders.json")
}

// getNotesPath returns the path to notes.json.
func getNotesPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "notes.json")
}

// SetReminderHandler sets a reminder.
func SetReminderHandler(args map[string]interface{}) (interface{}, error) {
	message, ok := args["message"].(string)
	if !ok || message == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'message' parameter is required",
			"Please provide a reminder message.",
			map[string]interface{}{
				"skill": "set_reminder",
				"field": "message",
			},
		), nil
	}

	timeStr, ok := args["time"].(string)
	if !ok || timeStr == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'time' parameter is required",
			"Please provide a time for the reminder (format: 'YYYY-MM-DD HH:MM' or 'HH:MM' for today).",
			map[string]interface{}{
				"skill": "set_reminder",
				"field": "time",
			},
		), nil
	}

	// Parse time string
	var reminderTime time.Time
	var err error

	// Try relative time first (e.g., "in 1 hour", "tomorrow at 3pm")
	now := time.Now()
	if strings.HasPrefix(timeStr, "in ") {
		// Simple relative parsing - could be enhanced
		return formatErrorResponse(
			"validation_error",
			"Relative time parsing not yet implemented",
			"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
			map[string]interface{}{
				"skill":    "convert_timezone",
				"field":    "time",
				"provided": timeStr,
			},
		), nil
	}

	// Try full datetime format
	if len(timeStr) > 10 {
		reminderTime, err = time.Parse("2006-01-02 15:04", timeStr)
		if err != nil {
			reminderTime, err = time.Parse("2006-01-02 15:04:05", timeStr)
		}
	} else {
		// Just time, use today
		timeLayout := "15:04"
		if len(strings.Split(timeStr, ":")) == 3 {
			timeLayout = "15:04:05"
		}
		parsedTime, err := time.Parse(timeLayout, timeStr)
		if err != nil {
			return nil, fmt.Errorf("invalid time format: %w", err)
		}
		reminderTime = time.Date(now.Year(), now.Month(), now.Day(), parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second(), 0, now.Location())
		if reminderTime.Before(now) {
			// If time has passed today, set for tomorrow
			reminderTime = reminderTime.Add(24 * time.Hour)
		}
	}

	if err != nil {
		return formatErrorResponse(
			"validation_error",
			"Failed to parse time",
			"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
			map[string]interface{}{
				"skill":    "set_reminder",
				"field":    "time",
				"provided": timeStr,
				"error":    err.Error(),
			},
		), nil
	}

	// Load existing reminders
	remindersPath := getRemindersPath()
	var reminders []Reminder
	if data, err := os.ReadFile(remindersPath); err == nil {
		// Ignore unmarshal error - if file is corrupt, start with empty list
		_ = json.Unmarshal(data, &reminders)
	}

	// Add new reminder
	reminder := Reminder{
		ID:      uuid.New().String(),
		Message: message,
		Time:    reminderTime,
		Created: now,
	}
	reminders = append(reminders, reminder)

	// Save reminders
	os.MkdirAll(filepath.Dir(remindersPath), 0755)
	data, err := json.MarshalIndent(reminders, "", "  ")
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to save reminder",
			"An internal error occurred while saving the reminder. Please try again.",
			map[string]interface{}{
				"skill": "set_reminder",
				"error": err.Error(),
			},
		), nil
	}
	if err := os.WriteFile(remindersPath, data, 0644); err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to save reminder file",
			"An internal error occurred while saving the reminder. Please try again.",
			map[string]interface{}{
				"skill": "set_reminder",
				"error": err.Error(),
			},
		), nil
	}

	return map[string]interface{}{
		"id":      reminder.ID,
		"message": message,
		"time":    reminderTime.Format(time.RFC3339),
		"success": true,
	}, nil
}

// ListRemindersHandler lists all reminders.
func ListRemindersHandler(args map[string]interface{}) (interface{}, error) {
	remindersPath := getRemindersPath()
	var reminders []Reminder
	if data, err := os.ReadFile(remindersPath); err == nil {
		// Ignore unmarshal error - if file is corrupt, return empty list
		_ = json.Unmarshal(data, &reminders)
	}

	// Filter active reminders (future only)
	now := time.Now()
	activeReminders := make([]map[string]interface{}, 0)
	for _, r := range reminders {
		if r.Time.After(now) {
			activeReminders = append(activeReminders, map[string]interface{}{
				"id":      r.ID,
				"message": r.Message,
				"time":    r.Time.Format(time.RFC3339),
				"created": r.Created.Format(time.RFC3339),
			})
		}
	}

	return map[string]interface{}{
		"count":     len(activeReminders),
		"reminders": activeReminders,
	}, nil
}

// SaveNoteHandler saves a note.
func SaveNoteHandler(args map[string]interface{}) (interface{}, error) {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'content' parameter is required",
			"Please provide the note content you want to save.",
			map[string]interface{}{
				"skill": "save_note",
				"field": "content",
			},
		), nil
	}

	title := ""
	if t, ok := args["title"].(string); ok {
		title = t
	} else {
		// Generate title from first line of content
		lines := strings.Split(content, "\n")
		title = strings.TrimSpace(lines[0])
		if len(title) > 50 {
			title = title[:50] + "..."
		}
	}

	// Load existing notes
	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err == nil {
		// Ignore unmarshal error - if file is corrupt, start with empty map
		_ = json.Unmarshal(data, &notes)
	} else {
		notes = make(map[string]Note)
	}

	// Save or update note
	now := time.Now()
	if existing, exists := notes[title]; exists {
		existing.Content = content
		existing.Updated = now
		notes[title] = existing
	} else {
		notes[title] = Note{
			Title:   title,
			Content: content,
			Created: now,
			Updated: now,
		}
	}

	// Save notes
	os.MkdirAll(filepath.Dir(notesPath), 0755)
	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to save note",
			"An internal error occurred while saving the note. Please try again.",
			map[string]interface{}{
				"skill": "save_note",
				"error": err.Error(),
			},
		), nil
	}
	if err := os.WriteFile(notesPath, data, 0644); err != nil {
		return formatErrorResponse(
			"internal_error",
			"Failed to save note file",
			"An internal error occurred while saving the note. Please try again.",
			map[string]interface{}{
				"skill": "save_note",
				"error": err.Error(),
			},
		), nil
	}

	return map[string]interface{}{
		"title":   title,
		"success": true,
	}, nil
}

// GetNoteHandler retrieves a note.
func GetNoteHandler(args map[string]interface{}) (interface{}, error) {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return formatErrorResponse(
			"validation_error",
			"The 'title' parameter is required",
			"Please provide the title of the note you want to retrieve.",
			map[string]interface{}{
				"skill": "get_note",
				"field": "title",
			},
		), nil
	}

	// Load notes
	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err != nil {
		return formatErrorResponse(
			"not_found",
			fmt.Sprintf("Note '%s' not found", title),
			"The note file does not exist or the note with this title was not found.",
			map[string]interface{}{
				"skill": "get_note",
				"title": title,
			},
		), nil
	} else {
		if err := json.Unmarshal(data, &notes); err != nil {
			return formatErrorResponse(
				"internal_error",
				"Failed to parse notes file",
				"The notes file may be corrupted. Please try again.",
				map[string]interface{}{
					"skill": "get_note",
					"error": err.Error(),
				},
			), nil
		}
	}

	note, exists := notes[title]
	if !exists {
		return formatErrorResponse(
			"not_found",
			fmt.Sprintf("Note '%s' not found", title),
			"No note exists with this title. Use 'list_notes' to see available notes.",
			map[string]interface{}{
				"skill": "get_note",
				"title": title,
			},
		), nil
	}

	return map[string]interface{}{
		"title":   note.Title,
		"content": note.Content,
		"created": note.Created.Format(time.RFC3339),
		"updated": note.Updated.Format(time.RFC3339),
	}, nil
}

// ListNotesHandler lists all notes.
func ListNotesHandler(args map[string]interface{}) (interface{}, error) {
	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err == nil {
		// Ignore unmarshal error - if file is corrupt, return empty map
		_ = json.Unmarshal(data, &notes)
	} else {
		notes = make(map[string]Note)
	}

	noteList := make([]map[string]interface{}, 0, len(notes))
	for _, note := range notes {
		noteList = append(noteList, map[string]interface{}{
			"title":   note.Title,
			"created": note.Created.Format(time.RFC3339),
			"updated": note.Updated.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"count": len(noteList),
		"notes": noteList,
	}, nil
}

// CreateDefaultSkillFiles creates default skill JSON files in ~/.celeste/skills/
func CreateDefaultSkillFiles() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	skillsDir := filepath.Join(homeDir, ".celeste", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}

	skills := []Skill{
		TarotSkill(),
	}

	for _, skill := range skills {
		data, err := json.MarshalIndent(skill, "", "  ")
		if err != nil {
			continue
		}

		path := filepath.Join(skillsDir, skill.Name+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, data, 0644)
		}
	}

	return nil
}
