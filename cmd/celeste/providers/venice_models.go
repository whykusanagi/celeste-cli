package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// veniceModelsURL is Venice's public model catalog. Each text model carries
// model_spec.capabilities.supportsFunctionCalling, which we use to decide tool
// capability accurately instead of guessing from the model name (the old
// "uncensored = no tools" heuristic was wrong both ways — e.g.
// venice-uncensored-1-2 DOES support tools, some e2ee-*-uncensored do not).
const veniceModelsURL = "https://api.venice.ai/api/v1/models"

var (
	veniceOnce    sync.Once
	veniceSupport map[string]bool // model id -> supportsFunctionCalling
)

// parseVeniceToolSupport parses a Venice /models response into a map of model id
// -> model_spec.capabilities.supportsFunctionCalling. Pure + testable.
func parseVeniceToolSupport(body []byte) map[string]bool {
	var payload struct {
		Data []struct {
			ID        string `json:"id"`
			ModelSpec struct {
				Capabilities struct {
					SupportsFunctionCalling bool `json:"supportsFunctionCalling"`
				} `json:"capabilities"`
			} `json:"model_spec"`
		} `json:"data"`
	}
	out := map[string]bool{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return out
	}
	for _, m := range payload.Data {
		out[m.ID] = m.ModelSpec.Capabilities.SupportsFunctionCalling
	}
	return out
}

// loadVeniceToolSupport fetches the live catalog once (best-effort, cached).
func loadVeniceToolSupport() map[string]bool {
	veniceOnce.Do(func() {
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(veniceModelsURL)
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
		veniceSupport = parseVeniceToolSupport(body)
	})
	return veniceSupport
}

// VeniceToolSupport reports whether a Venice model supports tool calling per the
// live catalog. known=false when the catalog is unavailable or the model isn't
// listed, so callers fall back to a heuristic.
func VeniceToolSupport(modelID string) (supported, known bool) {
	return lookupToolSupport(loadVeniceToolSupport(), modelID)
}
