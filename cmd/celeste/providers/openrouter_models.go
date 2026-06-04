package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// openRouterModelsURL is OpenRouter's public model catalog: every model with its
// pricing and capabilities, including `supported_parameters` (which lists
// "tools" when the model supports function calling). We use it to harden the
// tool-capability guardrail instead of guessing from the model name.
const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

var (
	orOnce    sync.Once
	orSupport map[string]bool // model id -> supports tool calling
)

// parseOpenRouterToolSupport parses an OpenRouter /models response into a map of
// model id -> whether `supported_parameters` includes "tools". Pure + testable.
func parseOpenRouterToolSupport(body []byte) map[string]bool {
	var payload struct {
		Data []struct {
			ID                  string   `json:"id"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	out := map[string]bool{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return out
	}
	for _, m := range payload.Data {
		for _, p := range m.SupportedParameters {
			if p == "tools" {
				out[m.ID] = true
				break
			}
		}
		if _, seen := out[m.ID]; !seen {
			out[m.ID] = false
		}
	}
	return out
}

// lookupToolSupport resolves a model id against the catalog, tolerating an
// OpenRouter ":variant" suffix (e.g. "meta-llama/llama-3:free"). known=false when
// the catalog is empty or the model isn't listed. Pure + testable.
func lookupToolSupport(catalog map[string]bool, modelID string) (supported, known bool) {
	if len(catalog) == 0 {
		return false, false
	}
	if v, ok := catalog[modelID]; ok {
		return v, true
	}
	if i := strings.IndexByte(modelID, ':'); i >= 0 {
		if v, ok := catalog[modelID[:i]]; ok {
			return v, true
		}
	}
	return false, false
}

// loadOpenRouterToolSupport fetches the live catalog once (best-effort, cached).
func loadOpenRouterToolSupport() map[string]bool {
	orOnce.Do(func() {
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(openRouterModelsURL)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return
		}
		orSupport = parseOpenRouterToolSupport(body)
	})
	return orSupport
}

// OpenRouterToolSupport reports whether an OpenRouter model supports tool calling
// per the live catalog. known=false when the catalog is unavailable or the model
// isn't listed, so callers fall back to a heuristic.
func OpenRouterToolSupport(modelID string) (supported, known bool) {
	return lookupToolSupport(loadOpenRouterToolSupport(), modelID)
}
