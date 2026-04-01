package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// WeatherTool gets weather forecast for a location.
type WeatherTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewWeatherTool creates a WeatherTool.
func NewWeatherTool(configLoader ConfigLoader) *WeatherTool {
	return &WeatherTool{
		BaseTool: BaseTool{
			ToolName:        "get_weather",
			ToolDescription: "Get current weather and forecast for a location. Uses default zip code if not specified. User can provide zip code in the prompt to override default.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"zip_code": map[string]any{
						"type":        "string",
						"description": "Optional zip code (5 digits). If not provided, uses default zip code from configuration. User can specify zip code in their message to override default.",
					},
					"days": map[string]any{
						"type":        "integer",
						"description": "Number of days for forecast (1-3, default: 1 for current weather)",
					},
				},
				"required": []string{},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
		configLoader: configLoader,
	}
}

func (t *WeatherTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	// Try to get config, but don't fail if it's not configured
	config, err := t.configLoader.GetWeatherConfig()
	if err != nil {
		config = WeatherConfig{}
	}

	// Get zip code - accept both string and number types
	var zipCode string
	var found bool

	if val, ok := input["zip_code"].(string); ok && val != "" {
		zipCode = val
		found = true
	} else if val, ok := input["zip_code"].(float64); ok {
		zipCode = fmt.Sprintf("%.0f", val)
		found = true
	} else {
		zipCode = config.DefaultZipCode
		found = zipCode != ""
	}

	if !found {
		return resultFromMap(formatConfigError("get_weather", "zip_code", "celeste config --set-weather-zip <zip>"))
	}

	// Validate zip code format (5 digits)
	if len(zipCode) != 5 {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"Zip code must be exactly 5 digits",
			"Please provide a valid 5-digit US zip code",
			map[string]any{
				"skill":    "get_weather",
				"field":    "zip_code",
				"provided": zipCode,
			},
		))
	}
	for _, c := range zipCode {
		if c < '0' || c > '9' {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"Zip code must contain only digits",
				"Please provide a valid 5-digit US zip code",
				map[string]any{
					"skill":    "get_weather",
					"field":    "zip_code",
					"provided": zipCode,
				},
			))
		}
	}

	days := 1
	if d, ok := input["days"].(float64); ok {
		days = int(d)
		if days < 1 {
			days = 1
		}
		if days > 3 {
			days = 3
		}
	}

	url := fmt.Sprintf("https://wttr.in/%s?format=j1", zipCode)
	if days > 1 {
		url = fmt.Sprintf("https://wttr.in/%s?format=j1&days=%d", zipCode, days)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to connect to weather service",
			"Please check your internet connection and try again.",
			map[string]any{
				"skill": "get_weather",
				"error": err.Error(),
			},
		))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("Weather API returned error (status %d)", resp.StatusCode),
			"The weather service may be temporarily unavailable. Please try again later.",
			map[string]any{
				"skill":       "get_weather",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse weather API response",
			"The weather service returned invalid data. Please try again.",
			map[string]any{
				"skill": "get_weather",
				"error": err.Error(),
			},
		))
	}

	result["zip_code"] = zipCode
	result["requested_days"] = days

	return resultFromMap(result)
}
