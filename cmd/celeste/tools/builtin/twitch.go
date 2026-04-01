package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TwitchTool checks if a Twitch streamer is live.
type TwitchTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewTwitchTool creates a TwitchTool.
func NewTwitchTool(configLoader ConfigLoader) *TwitchTool {
	return &TwitchTool{
		BaseTool: BaseTool{
			ToolName:        "check_twitch_live",
			ToolDescription: "Check if a Twitch streamer is currently live. Uses default streamer if not specified. User can provide streamer name in prompt to override default.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"streamer": map[string]any{
						"type":        "string",
						"description": "Optional Twitch streamer username. If not provided, uses default streamer from configuration. User can specify streamer name in their message to override default.",
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

func (t *TwitchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	config, err := t.configLoader.GetTwitchConfig()
	if err != nil {
		config = TwitchConfig{}
	}

	streamer, found := getUserOrDefault(input, "streamer", func() string {
		return config.DefaultStreamer
	})

	if !found {
		return resultFromMap(formatConfigError("check_twitch_live", "streamer", "celeste config --set-twitch-streamer <name>"))
	}

	if config.ClientID == "" || config.ClientSecret == "" {
		return resultFromMap(formatErrorResponse(
			"config_error",
			"Twitch Client ID and Secret are required. Please configure them in skills.json.",
			"The Twitch API requires OAuth authentication. You need both Client ID and Client Secret from the Twitch Developer Console.",
			map[string]any{
				"skill":          "check_twitch_live",
				"config_command": "Add twitch_client_id and twitch_client_secret to ~/.celeste/skills.json",
			},
		))
	}

	// Step 1: Get OAuth token
	tokenURL := "https://id.twitch.tv/oauth2/token"
	tokenData := fmt.Sprintf("client_id=%s&client_secret=%s&grant_type=client_credentials",
		config.ClientID, config.ClientSecret)

	client := &http.Client{Timeout: 10 * time.Second}
	tokenReq, err := http.NewRequest("POST", tokenURL, strings.NewReader(tokenData))
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to create OAuth request",
			"An internal error occurred. Please try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to get Twitch OAuth token",
			"Please check your internet connection and try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != 200 {
		body, _ := io.ReadAll(tokenResp.Body)
		return resultFromMap(formatErrorResponse(
			"auth_error",
			"Failed to authenticate with Twitch",
			"The Twitch Client ID or Secret may be invalid. Please check your configuration.",
			map[string]any{
				"skill":       "check_twitch_live",
				"status_code": tokenResp.StatusCode,
				"response":    string(body),
			},
		))
	}

	var tokenResult struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse OAuth token response",
			"The Twitch OAuth API returned invalid data. Please try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}

	// Step 2: Check if streamer is live
	url := fmt.Sprintf("https://api.twitch.tv/helix/streams?user_login=%s", streamer)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to create Twitch API request",
			"An internal error occurred. Please try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}

	req.Header.Set("Client-ID", config.ClientID)
	req.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to connect to Twitch API",
			"Please check your internet connection and try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("Twitch API returned error (status %d)", resp.StatusCode),
			"The Twitch API may be temporarily unavailable or the streamer may not exist.",
			map[string]any{
				"skill":       "check_twitch_live",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		))
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
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse Twitch API response",
			"The Twitch API returned invalid data. Please try again.",
			map[string]any{
				"skill": "check_twitch_live",
				"error": err.Error(),
			},
		))
	}

	isLive := len(result.Data) > 0

	response := map[string]any{
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

	return resultFromMap(response)
}
