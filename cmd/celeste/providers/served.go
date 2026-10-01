package providers

import (
	"context"
	"strings"
)

// Resolution runs in two steps so the TUI never waits on the network:
// PrepareModels (may block: startup, CLI, serve, or a tea.Cmd) loads what an
// endpoint serves into memory, and ResolveFromMemory (no I/O) decides.

// overridden reports a SetCatalogForTest catalog for provider.
func overridden(provider string) bool {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	_, found := catalogOverride[provider]
	return found
}

// PrepareModels loads an endpoint's catalog (the cache, or one fetch bounded
// by the timeout; a fetch that failed in the last five minutes is not
// retried) and, for providers that answer GET /models/{id}, checks each
// given model the catalog doesn't list. The answers stay in memory for
// ResolveFromMemory. It may block on the network: never call it from a
// Bubble Tea Update.
func PrepareModels(ctx context.Context, provider, baseURL, apiKey string, models ...string) {
	cat, ok := LoadCatalog(ctx, provider, baseURL, apiKey)
	if !ok || !hasModelEndpoint(provider) || overridden(provider) {
		return
	}
	key := catalogKey(provider, baseURL, apiKey)
	for _, id := range models {
		if id == "" || neverReplace(provider, id) {
			continue
		}
		if _, served := findServed(cat, id); served {
			continue
		}
		lower := strings.ToLower(id)
		catalogMu.Lock()
		st := stateLocked(key)
		_, done := st.verified[lower]
		failed := st.verifyFailedAt[lower]
		catalogMu.Unlock()
		if done || (!failed.IsZero() && catalogNow().Sub(failed) < catalogNegativeTTL) {
			continue
		}
		served, known := catalogVerify(ctx, provider, normalizeBaseURL(provider, baseURL), apiKey, id)
		catalogMu.Lock()
		if known {
			st.verified[lower] = served
			delete(st.verifyFailedAt, lower)
		} else {
			st.verifyFailedAt[lower] = catalogNow()
		}
		catalogMu.Unlock()
	}
}

// ResolveFromMemory resolves configured against what this process knows the
// endpoint serves, with no I/O. A model is replaced only when celeste is
// sure it's gone: the catalog doesn't list it and, where the provider can
// say, GET /models/{id} answered 404. pending is true when PrepareModels
// would know more (no catalog loaded yet, a stale one, or an unchecked
// miss); until then the configured model is kept.
func ResolveFromMemory(provider, baseURL, apiKey, configured string) (model, note string, pending bool) {
	key := catalogKey(provider, baseURL, apiKey)
	cat, stale, ok := MemoryCatalog(provider, baseURL, apiKey)
	if !ok {
		model, _ = ResolveModel(provider, configured, nil, false)
		return model, "", HasCatalog(provider) && !overridden(provider) && !fetchBlocked(key)
	}
	pending = stale && !fetchBlocked(key)
	if _, served := findServed(cat, configured); configured == "" || served || neverReplace(provider, configured) {
		model, note = ResolveModel(provider, configured, cat, true)
		return model, note, pending
	}
	if hasModelEndpoint(provider) && !overridden(provider) {
		lower := strings.ToLower(configured)
		catalogMu.Lock()
		st := stateLocked(key)
		answer, known := st.verified[lower]
		failed := st.verifyFailedAt[lower]
		catalogMu.Unlock()
		if !known {
			recentFailure := !failed.IsZero() && catalogNow().Sub(failed) < catalogNegativeTTL
			return configured, "", pending || !recentFailure
		}
		if answer {
			return configured, "", pending
		}
	}
	model, note = ResolveModel(provider, configured, cat, true)
	return model, note, pending
}
