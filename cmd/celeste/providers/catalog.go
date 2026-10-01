package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CatalogModel is one model a provider serves now, from its /models listing.
type CatalogModel struct {
	ID string `json:"id"`
	// Tools is the provider's own word on tool calling; nil when the listing
	// doesn't say (most OpenAI-compatible APIs).
	Tools *bool `json:"tools,omitempty"`
	// Default marks the model the provider itself flags as its default
	// (Venice's "default" trait).
	Default bool `json:"default,omitempty"`
}

const (
	// catalogTTL is how long a cached catalog counts as fresh. A stale one is
	// still used while a refresh runs.
	catalogTTL = 24 * time.Hour
	// catalogFetchTimeout bounds one /models request.
	catalogFetchTimeout = 4 * time.Second
	// anthropicVersion is the API version header Anthropic's REST API requires.
	anthropicVersion = "2023-06-01"
)

// errNoCatalog is returned for a provider celeste can't list models for.
var errNoCatalog = errors.New("provider has no model catalog")

// HasCatalog reports whether celeste can list the models a provider serves.
// Gemini, Vertex, DigitalOcean, local servers and ElevenLabs have none.
func HasCatalog(provider string) bool {
	caps, ok := Registry[provider]
	return ok && caps.SupportsModelListing
}

// catalogEntry is one cached catalog, in memory and on disk. It never holds
// the API key.
type catalogEntry struct {
	FetchedAt time.Time      `json:"fetched_at"`
	BaseURL   string         `json:"base_url"`
	Models    []CatalogModel `json:"models"`
}

var (
	catalogMu         sync.Mutex
	catalogMem        = map[string]catalogEntry{} // provider|baseURL -> entry
	catalogLatest     = map[string]string{}       // provider -> key last loaded
	catalogRefreshing = map[string]bool{}         // keys with a background refresh running
	catalogRefreshWG  sync.WaitGroup
	// catalogOverride holds SetCatalogForTest catalogs. A provider listed
	// here never reaches the disk cache or the network.
	catalogOverride = map[string][]CatalogModel{}

	catalogNow   = time.Now
	catalogDir   = defaultCatalogDir
	catalogFetch = fetchCatalog
)

// defaultCatalogDir is ~/.celeste/cache/models.
func defaultCatalogDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".celeste", "cache", "models")
}

// normalizeBaseURL defaults an empty base URL to the provider's registry URL
// and drops a trailing slash, so one endpoint has one cache key.
func normalizeBaseURL(provider, baseURL string) string {
	if baseURL == "" {
		baseURL = Registry[provider].BaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func catalogKey(provider, baseURL string) string {
	return provider + "|" + normalizeBaseURL(provider, baseURL)
}

// catalogCachePath is <dir>/<provider>-<short hash of base URL>.json.
func catalogCachePath(provider, baseURL string) string {
	dir := catalogDir()
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalizeBaseURL(provider, baseURL)))
	return filepath.Join(dir, provider+"-"+hex.EncodeToString(sum[:])[:12]+".json")
}

// CachedCatalog returns the cached catalog for an endpoint, from memory or
// the disk cache, without touching the network. stale is true when it is
// older than the TTL; the caller decides whether to refresh it.
func CachedCatalog(provider, baseURL string) (models []CatalogModel, stale, ok bool) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if m, found := catalogOverride[provider]; found {
		return m, false, len(m) > 0
	}
	if !HasCatalog(provider) {
		return nil, false, false
	}
	key := catalogKey(provider, baseURL)
	e, found := catalogMem[key]
	if !found {
		var err error
		if e, err = readCatalogFile(catalogCachePath(provider, baseURL)); err != nil {
			return nil, false, false
		}
		catalogMem[key] = e
	}
	catalogLatest[provider] = key
	return e.Models, catalogNow().Sub(e.FetchedAt) > catalogTTL, true
}

// CatalogFor returns the catalog most recently loaded in this process for a
// provider, whatever its endpoint. It never reads disk or the network: the
// tool gate calls it from the TUI's Update.
func CatalogFor(provider string) ([]CatalogModel, bool) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if m, found := catalogOverride[provider]; found {
		return m, len(m) > 0
	}
	e, found := catalogMem[catalogLatest[provider]]
	if !found {
		return nil, false
	}
	return e.Models, true
}

// LoadCatalog returns the endpoint's catalog: the cached one when there is
// one (a stale cache is refreshed in the background), else a synchronous
// fetch bounded by catalogFetchTimeout. ok is false when there is no catalog
// (unsupported provider, offline, no key). Never call it from a Bubble Tea
// Update: use CachedCatalog there and fetch in a tea.Cmd.
func LoadCatalog(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, bool) {
	if models, stale, ok := CachedCatalog(provider, baseURL); ok {
		if stale {
			refreshCatalogInBackground(provider, baseURL, apiKey)
		}
		return models, true
	}
	if !HasCatalog(provider) {
		return nil, false
	}
	models, err := RefreshCatalog(ctx, provider, baseURL, apiKey)
	return models, err == nil
}

// RefreshCatalog fetches the endpoint's catalog and caches it. A failed fetch
// leaves the existing cache alone.
func RefreshCatalog(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, error) {
	catalogMu.Lock()
	if m, found := catalogOverride[provider]; found {
		catalogMu.Unlock()
		if len(m) == 0 {
			return nil, errNoCatalog
		}
		return m, nil
	}
	catalogMu.Unlock()

	models, err := catalogFetch(ctx, provider, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	e := catalogEntry{FetchedAt: catalogNow(), BaseURL: normalizeBaseURL(provider, baseURL), Models: models}
	key := catalogKey(provider, baseURL)
	catalogMu.Lock()
	catalogMem[key] = e
	catalogLatest[provider] = key
	catalogMu.Unlock()
	// Best effort: an unwritable cache only costs a fetch next start.
	_ = writeCatalogFile(catalogCachePath(provider, baseURL), e)
	return models, nil
}

// refreshCatalogInBackground refreshes a stale catalog once per endpoint at a
// time.
func refreshCatalogInBackground(provider, baseURL, apiKey string) {
	key := catalogKey(provider, baseURL)
	catalogMu.Lock()
	if catalogRefreshing[key] {
		catalogMu.Unlock()
		return
	}
	catalogRefreshing[key] = true
	catalogRefreshWG.Add(1)
	catalogMu.Unlock()
	go func() {
		defer catalogRefreshWG.Done()
		defer func() {
			catalogMu.Lock()
			delete(catalogRefreshing, key)
			catalogMu.Unlock()
		}()
		_, _ = RefreshCatalog(context.Background(), provider, baseURL, apiKey)
	}()
}

// waitCatalogRefreshes blocks until background refreshes finish (tests).
func waitCatalogRefreshes() { catalogRefreshWG.Wait() }

// resetCatalogMemory forgets every in-memory catalog (tests).
func resetCatalogMemory() {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	catalogMem = map[string]catalogEntry{}
	catalogLatest = map[string]string{}
}

func readCatalogFile(path string) (catalogEntry, error) {
	var e catalogEntry
	if path == "" {
		return e, errNoCatalog
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	if len(e.Models) == 0 {
		return e, errNoCatalog
	}
	return e, nil
}

// writeCatalogFile writes the entry through a temp file and a rename, so a
// concurrent reader sees the old file or the new one. The directory is 0700
// and the file 0600.
func writeCatalogFile(path string, e catalogEntry) (err error) {
	if path == "" {
		return errNoCatalog
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	// Close before rename: Windows cannot rename an open file.
	if err = tmp.Close(); err != nil {
		return err
	}
	for i := 0; i < 20; i++ { // Windows: a reader holding the file blocks the rename briefly.
		if err = os.Rename(tmpName, path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// fetchCatalog is catalogFetch's real implementation: one GET of the
// provider's /models listing. Errors carry the status, never the key.
func fetchCatalog(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, error) {
	if !HasCatalog(provider) {
		return nil, errNoCatalog
	}
	base := normalizeBaseURL(provider, baseURL)
	if base == "" {
		return nil, errNoCatalog
	}
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("%s models: bad base URL", provider)
	}
	if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models: %w", provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s models: unexpected status %d", provider, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%s models: %w", provider, err)
	}
	return parseCatalog(provider, body)
}

// parseCatalog parses a /models response. Every supported shape lists models
// under data[].id; Venice adds model_spec (traits, capabilities) and
// OpenRouter adds supported_parameters. Pure + testable.
func parseCatalog(provider string, body []byte) ([]CatalogModel, error) {
	var payload struct {
		Data []struct {
			ID                  string   `json:"id"`
			Type                string   `json:"type"`
			SupportedParameters []string `json:"supported_parameters"`
			ModelSpec           *struct {
				Traits       []string `json:"traits"`
				Capabilities *struct {
					SupportsFunctionCalling *bool `json:"supportsFunctionCalling"`
				} `json:"capabilities"`
			} `json:"model_spec"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%s models: unreadable response", provider)
	}
	var out []CatalogModel
	for _, d := range payload.Data {
		if d.ID == "" {
			continue
		}
		m := CatalogModel{ID: d.ID}
		switch provider {
		case "venice":
			// Venice lists image, audio and embedding models too; only text
			// models can chat.
			if d.Type != "" && d.Type != "text" {
				continue
			}
			if d.ModelSpec != nil {
				for _, t := range d.ModelSpec.Traits {
					if t == "default" {
						m.Default = true
					}
				}
				if c := d.ModelSpec.Capabilities; c != nil && c.SupportsFunctionCalling != nil {
					v := *c.SupportsFunctionCalling
					m.Tools = &v
				} else {
					f := false
					m.Tools = &f
				}
			}
		case "openrouter":
			v := false
			for _, p := range d.SupportedParameters {
				if p == "tools" {
					v = true
					break
				}
			}
			m.Tools = &v
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s models: empty catalog", provider)
	}
	return out, nil
}

// CatalogToolSupport reports what the provider's own catalog says about a
// model's tool calling. known is false when no catalog is loaded, the model
// isn't listed, or the listing doesn't say. An OpenRouter ":variant" suffix
// falls back to the base id.
func CatalogToolSupport(provider, modelID string) (supported, known bool) {
	cat, ok := CatalogFor(provider)
	if !ok {
		return false, false
	}
	if m, found := findServed(cat, modelID); found && m.Tools != nil {
		return *m.Tools, true
	}
	return false, false
}

// findServed looks a model up in a catalog, tolerating a ":variant" suffix
// (OpenRouter's :free, :nitro, ...).
func findServed(cat []CatalogModel, modelID string) (CatalogModel, bool) {
	if modelID == "" {
		return CatalogModel{}, false
	}
	for _, m := range cat {
		if m.ID == modelID {
			return m, true
		}
	}
	if i := strings.IndexByte(modelID, ':'); i > 0 {
		base := modelID[:i]
		for _, m := range cat {
			if m.ID == base {
				return m, true
			}
		}
	}
	return CatalogModel{}, false
}
