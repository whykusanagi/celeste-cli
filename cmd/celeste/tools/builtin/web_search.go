package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// maxSearchesPerSession is the per-session rate limit for web searches.
const maxSearchesPerSession = 10

// WebSearchTool performs web searches via DuckDuckGo HTML.
type WebSearchTool struct {
	BaseTool

	mu           sync.Mutex
	searchCount  int
}

// NewWebSearchTool creates a WebSearchTool.
func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		BaseTool: BaseTool{
			ToolName:        "web_search",
			ToolDescription: "Search the web using DuckDuckGo. Returns up to 10 results with titles, URLs, and snippets. Limited to 10 searches per session.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query string.",
					},
				},
				"required": []string{"query"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"query"},
		},
	}
}

// searchResult holds a single search result entry.
type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	query := getStringArg(input, "query", "")
	if query == "" {
		return resultFromMap(formatErrorResponse("validation_error", "query is required", "", nil))
	}

	// Rate limit check
	t.mu.Lock()
	if t.searchCount >= maxSearchesPerSession {
		t.mu.Unlock()
		return resultFromMap(formatErrorResponse(
			"rate_limit",
			fmt.Sprintf("Search limit reached (%d/%d per session)", maxSearchesPerSession, maxSearchesPerSession),
			"You have used all available web searches for this session.",
			nil,
		))
	}
	t.searchCount++
	t.mu.Unlock()

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to create request", err.Error(), nil))
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CelesteCLI/1.8)")

	resp, err := client.Do(req)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to fetch search results", err.Error(), nil))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("DuckDuckGo returned status %d", resp.StatusCode),
			"The search service may be temporarily unavailable.",
			nil,
		))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to read response body", err.Error(), nil))
	}

	results := parseDDGResults(string(body))

	return resultFromMap(map[string]any{
		"query":   query,
		"results": results,
	})
}

// parseDDGResults extracts search results from DuckDuckGo HTML.
func parseDDGResults(html string) []searchResult {
	var results []searchResult

	// DuckDuckGo HTML results have result__title containing <a> with href,
	// and result__snippet containing the snippet text.

	// Match result blocks: look for result__title and result__snippet patterns
	titleRe := regexp.MustCompile(`class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetRe := regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</(?:a|td|span)`)

	titleMatches := titleRe.FindAllStringSubmatch(html, 10)
	snippetMatches := snippetRe.FindAllStringSubmatch(html, 10)

	for i, m := range titleMatches {
		if len(m) < 3 {
			continue
		}
		rawURL := m[1]
		title := stripHTMLTags(m[2])

		// DuckDuckGo wraps URLs in a redirect; extract the actual URL
		if u, err := url.Parse(rawURL); err == nil {
			if actual := u.Query().Get("uddg"); actual != "" {
				rawURL = actual
			}
		}

		snippet := ""
		if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
			snippet = stripHTMLTags(snippetMatches[i][1])
		}

		results = append(results, searchResult{
			Title:   strings.TrimSpace(title),
			URL:     rawURL,
			Snippet: strings.TrimSpace(snippet),
		})
	}

	return results
}

// stripHTMLTags removes HTML tags from a string.
func stripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}
