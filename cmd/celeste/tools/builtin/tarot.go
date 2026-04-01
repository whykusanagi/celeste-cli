package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TarotTool generates tarot card readings.
type TarotTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewTarotTool creates a TarotTool.
func NewTarotTool(configLoader ConfigLoader) *TarotTool {
	return &TarotTool{
		BaseTool: BaseTool{
			ToolName:        "tarot_reading",
			ToolDescription: "Generate a tarot card reading using either a three-card spread (past/present/future) or a celtic cross spread",
			ToolParameters: mustJSON(map[string]interface{}{
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
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"spread_type"},
		},
		configLoader: configLoader,
	}
}

func (t *TarotTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	config, err := t.configLoader.GetTarotConfig()
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"config_error",
			"Tarot configuration is required. Please configure it using: celeste config --set-tarot-token <token>",
			"The tarot auth token is needed to access the tarot reading service.",
			map[string]interface{}{
				"skill":          "tarot_reading",
				"config_command": "celeste config --set-tarot-token <token>",
			},
		))
	}

	spreadType := "three"
	if st, ok := input["spread_type"].(string); ok {
		spreadType = st
	}

	question := ""
	if q, ok := input["question"].(string); ok {
		question = q
	}

	requestBody := map[string]interface{}{
		"spread_type": spreadType,
	}
	if question != "" {
		requestBody["question"] = question
	}

	reqBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to encode tarot request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "tarot_reading",
				"error": err.Error(),
			},
		))
	}

	req, err := http.NewRequest("POST", config.FunctionURL, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to create tarot request",
			"An internal error occurred. Please try again.",
			map[string]interface{}{
				"skill": "tarot_reading",
				"error": err.Error(),
			},
		))
	}

	authToken := config.AuthToken
	if !strings.HasPrefix(authToken, "Basic ") {
		authToken = "Basic " + authToken
	}
	req.Header.Set("Authorization", authToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	startTime := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(startTime)

	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to connect to tarot API",
			"Please check your internet connection and try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		))
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to read tarot API response",
			"The tarot service may have returned invalid data. Please try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		))
	}

	if resp.StatusCode != 200 {
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("Tarot API returned error (status %d)", resp.StatusCode),
			"The tarot reading service may be temporarily unavailable. Please try again later.",
			map[string]interface{}{
				"skill":       "tarot_reading",
				"status_code": resp.StatusCode,
				"response":    string(responseBody),
				"elapsed":     elapsed.String(),
			},
		))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse tarot API response",
			"The tarot service returned invalid data. Please try again.",
			map[string]interface{}{
				"skill":   "tarot_reading",
				"error":   err.Error(),
				"elapsed": elapsed.String(),
			},
		))
	}

	return resultFromMap(result)
}
