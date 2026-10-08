package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// CollectionsSearchTool searches xAI collections using the documents/search API.
// This is the explicit RAG search — not the implicit collection_ids parameter
// on chat completions, which xAI doesn't reliably use for retrieval.
type CollectionsSearchTool struct {
	BaseTool
	apiKey        string
	collectionIDs []string
	// endpoint is the documents/search URL; tests point it at a fake.
	endpoint string
}

// collectionsSearchEndpoint is xAI's documents/search API.
const collectionsSearchEndpoint = "https://api.x.ai/v1/documents/search"

// NewCollectionsSearchTool creates a collections search tool.
func NewCollectionsSearchTool(cfg *config.Config) *CollectionsSearchTool {
	var collIDs []string
	if cfg.Collections != nil {
		// A copy: the tool's scope is what was active when it was built.
		collIDs = append([]string(nil), cfg.Collections.ActiveCollections...)
	}

	return &CollectionsSearchTool{
		BaseTool: BaseTool{
			ToolName: "collections_search",
			ToolDescription: "Search your xAI collections for relevant documents.\n\n" +
				"This performs explicit RAG retrieval against your active collections " +
				"(personality data, voicelines, user profiles, knowledge bases, etc.).\n" +
				"Returns matching document chunks with relevance scores.\n\n" +
				"Use this when asked about collection content, personality data, " +
				"voicelines, user info, or any content stored in your collections.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Search query — describe what you're looking for"
					},
					"collection_id": {
						"type": "string",
						"description": "One of the active collections to search (optional — searches all active if omitted)"
					}
				},
				"required": ["query"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
		apiKey:        cfg.APIKey,
		collectionIDs: collIDs,
		endpoint:      collectionsSearchEndpoint,
	}
}

type collectionSearchResult struct {
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
	FileID   string  `json:"file_id,omitempty"`
	FileName string  `json:"file_name,omitempty"`
}

func (t *CollectionsSearchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	query := getStringArg(input, "query", "")
	if query == "" {
		return tools.ToolResult{Error: true, Content: "query is required"}, nil
	}

	collIDs := t.collectionIDs
	if specificID := getStringArg(input, "collection_id", ""); specificID != "" {
		if !slices.Contains(t.collectionIDs, specificID) {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf(
				"collection %q is not an active collection; search one of the active collections or omit collection_id. Use /collections to enable more.", specificID)}, nil
		}
		collIDs = []string{specificID}
	}

	if len(collIDs) == 0 {
		return tools.ToolResult{Error: true, Content: "No active collections. Use /collections to enable some."}, nil
	}

	// Build request to xAI documents/search API
	reqBody := map[string]any{
		"query": query,
		"source": map[string]any{
			"collection_ids": collIDs,
		},
		"retrieval_mode": map[string]string{
			"type": "hybrid",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("marshal error: %v", err)}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("request error: %v", err)}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("API error: %v", err)}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("read error: %v", err)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("API error %d: %s", resp.StatusCode, string(body))}, nil
	}

	// Parse response — xAI returns "matches" array
	var apiResp struct {
		Matches []struct {
			ChunkContent string            `json:"chunk_content"`
			Score        float64           `json:"score"`
			FileID       string            `json:"file_id"`
			Fields       map[string]string `json:"fields,omitempty"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return tools.ToolResult{Content: string(body)}, nil
	}

	if len(apiResp.Matches) == 0 {
		return tools.ToolResult{Content: "No matching documents found in collections."}, nil
	}

	// Format results — deduplicate chunks from same file
	seen := make(map[string]bool)
	var results []collectionSearchResult
	for _, r := range apiResp.Matches {
		if seen[r.FileID] {
			continue
		}
		seen[r.FileID] = true
		title := r.Fields["title"]
		results = append(results, collectionSearchResult{
			Content:  r.ChunkContent,
			Score:    r.Score,
			FileID:   r.FileID,
			FileName: title,
		})
	}

	out, _ := json.MarshalIndent(map[string]any{
		"query":   query,
		"results": results,
		"count":   len(results),
	}, "", "  ")

	return tools.ToolResult{Content: string(out)}, nil
}
