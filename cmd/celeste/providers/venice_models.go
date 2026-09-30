package providers

import (
	"encoding/json"
	"fmt"
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

	// veniceFetchCatalog is the actual network call, behind a seam so tests
	// can replace it (StubVeniceToolCatalogForTest) instead of hitting the
	// live api.venice.ai (#151 W6b review, M4).
	veniceFetchCatalog = fetchVeniceToolCatalog
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

// fetchVeniceToolCatalog is veniceFetchCatalog's real implementation: one
// live GET against veniceModelsURL, parsed into a model id -> tool-support
// map.
func fetchVeniceToolCatalog() (map[string]bool, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(veniceModelsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("venice models: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseVeniceToolSupport(body), nil
}

// loadVeniceToolSupport fetches the live catalog once (best-effort, cached).
func loadVeniceToolSupport() map[string]bool {
	veniceOnce.Do(func() {
		if catalog, err := veniceFetchCatalog(); err == nil {
			veniceSupport = catalog
		}
	})
	return veniceSupport
}

// StubVeniceToolCatalogForTest replaces the live Venice catalog fetch with a
// fixed map and resets the sync.Once cache, so tests never reach
// api.venice.ai (#151 W6b review, M4). Call it with defer; the returned func
// restores the real fetch and clears the stubbed cache. Test-only — it lives
// here, not in a _test.go file, because tests in other packages (e.g. tui's
// venice_tools_gate_test.go) need it too, and a _test.go file's exports
// don't cross package boundaries.
func StubVeniceToolCatalogForTest(catalog map[string]bool) func() {
	origFetch := veniceFetchCatalog
	veniceFetchCatalog = func() (map[string]bool, error) { return catalog, nil }
	veniceOnce = sync.Once{}
	veniceSupport = nil
	return func() {
		veniceFetchCatalog = origFetch
		veniceOnce = sync.Once{}
		veniceSupport = nil
	}
}

// VeniceToolSupport reports whether a Venice model supports tool calling per the
// live catalog. known=false when the catalog is unavailable or the model isn't
// listed, so callers fall back to a heuristic.
func VeniceToolSupport(modelID string) (supported, known bool) {
	return lookupToolSupport(loadVeniceToolSupport(), modelID)
}

// WarmVeniceToolCatalog fetches and caches the live Venice catalog (the same
// sync.Once-guarded fetch VeniceToolSupport uses) without blocking on the
// result. Callers that know they're about to need it — the chat starting up
// on a Venice profile — run this in a goroutine as early as possible so the
// later synchronous call in the TUI's per-model tool gate (#151 W6b) is more
// likely to find the catalog already cached instead of blocking on a cold
// network fetch (up to 4s) from inside a UI update handler.
func WarmVeniceToolCatalog() { loadVeniceToolSupport() }
