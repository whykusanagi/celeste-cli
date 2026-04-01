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

// YouTubeTool gets recent videos from a YouTube channel.
type YouTubeTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewYouTubeTool creates a YouTubeTool.
func NewYouTubeTool(configLoader ConfigLoader) *YouTubeTool {
	return &YouTubeTool{
		BaseTool: BaseTool{
			ToolName:        "get_youtube_videos",
			ToolDescription: "Get recent videos from a YouTube channel. Uses default channel if not specified. User can provide channel name/ID in prompt to override default.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{
						"type":        "string",
						"description": "Optional YouTube channel username or channel ID. If not provided, uses default channel from configuration. User can specify channel in their message to override default.",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of videos to return (default: 5, min: 1, max: 50)",
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

func (t *YouTubeTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	config, err := t.configLoader.GetYouTubeConfig()
	if err != nil {
		config = YouTubeConfig{}
	}

	channel, found := getUserOrDefault(input, "channel", func() string {
		return config.DefaultChannel
	})

	if !found {
		return resultFromMap(formatConfigError("get_youtube_videos", "channel", "celeste config --set-youtube-channel <name>"))
	}

	if config.APIKey == "" {
		return resultFromMap(formatErrorResponse(
			"config_error",
			"YouTube API key is required. Please configure it using: celeste config --set-youtube-key <api-key>",
			"The YouTube API key is needed to access the YouTube Data API. You can get one from the Google Cloud Console.",
			map[string]any{
				"skill":          "get_youtube_videos",
				"config_command": "celeste config --set-youtube-key <api-key>",
			},
		))
	}

	maxResults := 5
	if m, ok := input["max_results"].(float64); ok {
		maxResults = int(m)
		if maxResults < 1 {
			maxResults = 1
		}
		if maxResults > 50 {
			maxResults = 50
		}
	}

	channelID := channel

	if !strings.HasPrefix(channel, "UC") && len(channel) != 24 {
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

	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/search?part=snippet&channelId=%s&order=date&type=video&maxResults=%d&key=%s", channelID, maxResults, config.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to connect to YouTube API",
			"Please check your internet connection and try again.",
			map[string]any{
				"skill": "get_youtube_videos",
				"error": err.Error(),
			},
		))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("YouTube API returned error (status %d)", resp.StatusCode),
			"The YouTube API may be temporarily unavailable or the channel may not exist.",
			map[string]any{
				"skill":       "get_youtube_videos",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		))
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
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse YouTube API response",
			"The YouTube API returned invalid data. Please try again.",
			map[string]any{
				"skill": "get_youtube_videos",
				"error": err.Error(),
			},
		))
	}

	videos := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		videos = append(videos, map[string]any{
			"video_id":      item.ID.VideoID,
			"title":         item.Snippet.Title,
			"description":   item.Snippet.Description,
			"published_at":  item.Snippet.PublishedAt.Format(time.RFC3339),
			"thumbnail_url": item.Snippet.Thumbnails.Default.URL,
			"channel_title": item.Snippet.ChannelTitle,
			"url":           fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID.VideoID),
		})
	}

	return resultFromMap(map[string]any{
		"channel":    channel,
		"channel_id": channelID,
		"count":      len(videos),
		"videos":     videos,
	})
}
