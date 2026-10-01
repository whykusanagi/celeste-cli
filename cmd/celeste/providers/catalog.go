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
	"net/url"
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

// catalogNegativeTTL is how long a failed fetch (or an unanswered
// per-model check) is remembered, so a server or CLI loop doesn't retry an
// unreachable endpoint on every request.
const catalogNegativeTTL = 5 * time.Minute

// endpointState is everything this process knows about one endpoint's
// models. Keyed by provider, cleaned base URL and a hash of the API key.
type endpointState struct {
	entry    *catalogEntry
	failedAt time.Time // last failed catalog fetch
	// verified holds per-model answers from GET /models/{id}, by lower-case
	// ID: true served, false retired (404).
	verified       map[string]bool
	verifyFailedAt map[string]time.Time
}

var (
	catalogMu         sync.Mutex
	catalogStates     = map[string]*endpointState{}
	catalogLatest     = map[string]string{} // provider -> key last loaded
	catalogRefreshing = map[string]bool{}   // keys with a background refresh running
	catalogRefreshWG  sync.WaitGroup
	// catalogOverride holds SetCatalogForTest catalogs. A provider listed
	// here never reaches the disk cache or the network, and its list is
	// taken as complete: a miss counts as retired.
	catalogOverride = map[string][]CatalogModel{}

	catalogNow    = time.Now
	catalogDir    = defaultCatalogDir
	catalogFetch  = fetchCatalog
	catalogVerify = verifyModel
)

// defaultCatalogDir is ~/.celeste/cache/models.
func defaultCatalogDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".celeste", "cache", "models")
}

// normalizeBaseURL defaults an empty base URL to the provider's registry
// URL, drops any userinfo, query and fragment (they never reach the cache,
// a log or a /models request), and drops a trailing slash, so one endpoint
// has one cache key.
func normalizeBaseURL(provider, baseURL string) string {
	if baseURL == "" {
		baseURL = Registry[provider].BaseURL
	}
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
		baseURL = u.String()
	} else if i := strings.IndexAny(baseURL, "?#"); i >= 0 {
		baseURL = baseURL[:i]
	}
	return strings.TrimRight(baseURL, "/")
}

// CleanBaseURL is a base URL safe to log: no userinfo, query or fragment.
func CleanBaseURL(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	return normalizeBaseURL("", baseURL)
}

// keyHash is a short, one-way tag of an API key: catalogs fetched with
// different keys never mix, and the key itself is never stored.
func keyHash(apiKey string) string {
	if apiKey == "" {
		return "nokey"
	}
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])[:8]
}

func catalogKey(provider, baseURL, apiKey string) string {
	return provider + "|" + normalizeBaseURL(provider, baseURL) + "|" + keyHash(apiKey)
}

// EndpointID names one endpoint's catalog (provider, base URL, key hash)
// without the key, so callers can tell whether two lookups share one.
func EndpointID(provider, baseURL, apiKey string) string {
	return catalogKey(provider, baseURL, apiKey)
}

// catalogCachePath is <dir>/<provider>-<base URL hash>-<key hash>.json.
func catalogCachePath(provider, baseURL, apiKey string) string {
	dir := catalogDir()
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalizeBaseURL(provider, baseURL)))
	return filepath.Join(dir, provider+"-"+hex.EncodeToString(sum[:])[:12]+"-"+keyHash(apiKey)+".json")
}

// stateLocked returns the endpoint's state, creating it. catalogMu held.
func stateLocked(key string) *endpointState {
	st := catalogStates[key]
	if st == nil {
		st = &endpointState{verified: map[string]bool{}, verifyFailedAt: map[string]time.Time{}}
		catalogStates[key] = st
	}
	return st
}

// CachedCatalog returns the cached catalog for an endpoint, from memory or
// the disk cache, without touching the network. stale is true when it is
// older than the TTL; the caller decides whether to refresh it.
func CachedCatalog(provider, baseURL, apiKey string) (models []CatalogModel, stale, ok bool) {
	return cachedCatalog(provider, baseURL, apiKey, true)
}

// MemoryCatalog is CachedCatalog without the disk: it only sees catalogs
// already loaded in this process, so the TUI's Update can call it.
func MemoryCatalog(provider, baseURL, apiKey string) (models []CatalogModel, stale, ok bool) {
	return cachedCatalog(provider, baseURL, apiKey, false)
}

func cachedCatalog(provider, baseURL, apiKey string, disk bool) (models []CatalogModel, stale, ok bool) {
	catalogMu.Lock()
	if m, found := catalogOverride[provider]; found {
		catalogMu.Unlock()
		return m, false, len(m) > 0
	}
	if !HasCatalog(provider) {
		catalogMu.Unlock()
		return nil, false, false
	}
	key := catalogKey(provider, baseURL, apiKey)
	var e *catalogEntry
	if st := catalogStates[key]; st != nil {
		e = st.entry
	}
	catalogMu.Unlock()
	if e == nil {
		if !disk {
			return nil, false, false
		}
		fromDisk, err := readCatalogFile(catalogCachePath(provider, baseURL, apiKey))
		if err != nil {
			return nil, false, false
		}
		catalogMu.Lock()
		st := stateLocked(key)
		if st.entry == nil { // else a fetch landed while we read; it is newer
			st.entry = &fromDisk
		}
		e = st.entry
		catalogMu.Unlock()
	}
	catalogMu.Lock()
	catalogLatest[provider] = key
	catalogMu.Unlock()
	return e.Models, catalogNow().Sub(e.FetchedAt) > catalogTTL, true
}

// SameEndpoint reports whether two base URLs name the same catalog.
func SameEndpoint(provider, a, b string) bool {
	return normalizeBaseURL(provider, a) == normalizeBaseURL(provider, b)
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
	st := catalogStates[catalogLatest[provider]]
	if st == nil || st.entry == nil {
		return nil, false
	}
	return st.entry.Models, true
}

// fetchBlocked reports whether a fetch for key failed within the negative
// window.
func fetchBlocked(key string) bool {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	st := catalogStates[key]
	return st != nil && !st.failedAt.IsZero() && catalogNow().Sub(st.failedAt) < catalogNegativeTTL
}

// LoadCatalog returns the endpoint's catalog: the cached one when there is
// one (a stale cache is refreshed in the background), else a synchronous
// fetch bounded by catalogFetchTimeout. A fetch that failed less than five
// minutes ago is not retried. ok is false when there is no catalog. Never
// call it from a Bubble Tea Update.
func LoadCatalog(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, bool) {
	if models, stale, ok := CachedCatalog(provider, baseURL, apiKey); ok {
		if stale && !fetchBlocked(catalogKey(provider, baseURL, apiKey)) {
			refreshCatalogInBackground(provider, baseURL, apiKey)
		}
		return models, true
	}
	if !HasCatalog(provider) || fetchBlocked(catalogKey(provider, baseURL, apiKey)) {
		return nil, false
	}
	models, err := RefreshCatalog(ctx, provider, baseURL, apiKey)
	return models, err == nil
}

// RefreshCatalog fetches the endpoint's catalog and caches it. A failed fetch
// leaves the existing cache alone and is remembered for the negative window.
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

	key := catalogKey(provider, baseURL, apiKey)
	models, err := catalogFetch(ctx, provider, normalizeBaseURL(provider, baseURL), apiKey)
	if err != nil {
		catalogMu.Lock()
		stateLocked(key).failedAt = catalogNow()
		catalogMu.Unlock()
		return nil, err
	}
	e := catalogEntry{FetchedAt: catalogNow(), BaseURL: normalizeBaseURL(provider, baseURL), Models: models}
	catalogMu.Lock()
	st := stateLocked(key)
	st.entry, st.failedAt = &e, time.Time{}
	catalogLatest[provider] = key
	catalogMu.Unlock()
	// Best effort: an unwritable cache only costs a fetch next start.
	_ = writeCatalogFile(catalogCachePath(provider, baseURL, apiKey), e)
	return models, nil
}

// refreshCatalogInBackground refreshes a stale catalog once per endpoint at a
// time.
func refreshCatalogInBackground(provider, baseURL, apiKey string) {
	key := catalogKey(provider, baseURL, apiKey)
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
	catalogStates = map[string]*endpointState{}
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
	sweepStaleTemps(dir)
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

// providerGet does one authenticated GET against the provider. Errors carry
// the status, never the key.
func providerGet(ctx context.Context, provider, rawURL, apiKey string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("%s models: bad base URL", provider)
	}
	if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s models: %w", provider, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("%s models: %w", provider, err)
	}
	return resp.StatusCode, body, nil
}

// sweepStaleTemps removes temp files a crashed write left behind, once they
// are an hour old (a younger one may be a write in progress).
func sweepStaleTemps(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, ".*.json.tmp-*"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && catalogNow().Sub(fi.ModTime()) > time.Hour {
			_ = os.Remove(m)
		}
	}
}

// fetchCatalog is catalogFetch's real implementation: GET {base}/models.
// Anthropic's listing paginates; every page is read.
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
	if provider != "anthropic" {
		status, body, err := providerGet(ctx, provider, base+"/models", apiKey)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("%s models: unexpected status %d", provider, status)
		}
		return parseCatalog(provider, body)
	}
	var out []CatalogModel
	after := ""
	for page := 0; page < 50; page++ {
		q := url.Values{"limit": {"1000"}}
		if after != "" {
			q.Set("after_id", after)
		}
		status, body, err := providerGet(ctx, provider, base+"/models?"+q.Encode(), apiKey)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("%s models: unexpected status %d", provider, status)
		}
		models, err := parseCatalog(provider, body)
		if err != nil {
			return nil, err
		}
		out = append(out, models...)
		var more struct {
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		_ = json.Unmarshal(body, &more)
		if !more.HasMore || more.LastID == "" || more.LastID == after {
			break
		}
		after = more.LastID
	}
	return out, nil
}

// HasModelEndpoint reports whether the provider answers GET /models/{id}.
func HasModelEndpoint(provider string) bool { return hasModelEndpoint(provider) }

// FindServed looks a model up in a catalog the way resolution does (case
// ignored, OpenRouter :variant rules).
func FindServed(cat []CatalogModel, modelID string) (CatalogModel, bool) {
	return findServed(cat, modelID)
}

// hasModelEndpoint reports whether the provider answers GET /models/{id},
// so a catalog miss can be checked before a model is called retired.
// Anthropic, OpenAI and xAI do; aliases they accept are often not listed.
func hasModelEndpoint(provider string) bool {
	switch provider {
	case "anthropic", "openai", "grok":
		return true
	}
	return false
}

// verifyModel is catalogVerify's real implementation: GET {base}/models/{id}.
// 200 is served, 404 retired; anything else (other statuses, timeouts) is
// unknown.
func verifyModel(ctx context.Context, provider, baseURL, apiKey, id string) (served, known bool) {
	base := normalizeBaseURL(provider, baseURL)
	if base == "" {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	status, _, err := providerGet(ctx, provider, base+"/models/"+url.PathEscape(id), apiKey)
	switch {
	case err != nil:
		return false, false
	case status == http.StatusOK:
		return true, true
	case status == http.StatusNotFound:
		return false, true
	}
	return false, false
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

// findServed looks a model up in a catalog, ignoring case. A ":variant"
// (OpenRouter's :free, :nitro, ...) falls back to its base only when the
// catalog lists no variant of that base at all: then the suffix is a routing
// option, while a listed family of variants means this one is gone.
func findServed(cat []CatalogModel, modelID string) (CatalogModel, bool) {
	if modelID == "" {
		return CatalogModel{}, false
	}
	for _, m := range cat {
		if strings.EqualFold(m.ID, modelID) {
			return m, true
		}
	}
	if i := strings.IndexByte(modelID, ':'); i > 0 {
		base := modelID[:i]
		var baseModel *CatalogModel
		for j, m := range cat {
			if strings.EqualFold(m.ID, base) {
				baseModel = &cat[j]
			}
			if len(m.ID) > len(base) && strings.EqualFold(m.ID[:len(base)+1], base+":") {
				return CatalogModel{}, false
			}
		}
		if baseModel != nil {
			return *baseModel, true
		}
	}
	return CatalogModel{}, false
}
