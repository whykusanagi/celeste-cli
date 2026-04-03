package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// maxFetchBytes is the maximum output size for web_fetch results (32KB).
const maxFetchBytes = 32 * 1024

// WebFetchTool fetches a URL and converts the HTML content to markdown.
type WebFetchTool struct {
	BaseTool
}

// NewWebFetchTool creates a WebFetchTool.
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		BaseTool: BaseTool{
			ToolName:        "web_fetch",
			ToolDescription: "Fetch a URL and convert its HTML content to markdown. Optionally provide a prompt to guide extraction. Output is capped at 32KB.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to fetch.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional extraction guidance to prepend to the output.",
					},
				},
				"required": []string{"url"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"url"},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	rawURL := getStringArg(input, "url", "")
	if rawURL == "" {
		return resultFromMap(formatErrorResponse("validation_error", "url is required", "", nil))
	}

	prompt := getStringArg(input, "prompt", "")

	// Validate URL scheme
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to create request", err.Error(), nil))
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CelesteCLI/1.8)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to fetch URL", err.Error(), nil))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("URL returned status %d", resp.StatusCode),
			"The URL may be unavailable or require authentication.",
			map[string]any{"url": rawURL, "status_code": resp.StatusCode},
		))
	}

	// Read body (limit to 1MB to avoid OOM on huge pages)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to read response body", err.Error(), nil))
	}

	// Convert HTML to markdown
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(string(body))
	if err != nil {
		// Fall back to raw text if conversion fails
		markdown = string(body)
	}

	// Prepend extraction prompt if provided
	if prompt != "" {
		markdown = fmt.Sprintf("## Extraction Guidance\n%s\n\n---\n\n%s", prompt, markdown)
	}

	// Cap output at maxFetchBytes
	if len(markdown) > maxFetchBytes {
		markdown = markdown[:maxFetchBytes] + "\n\n[Content truncated at 32KB]"
	}

	return resultFromMap(map[string]any{
		"url":     rawURL,
		"content": markdown,
	})
}
