package providers

import "context"

// SetCatalogForTest is for tests only. It makes provider's catalog the given
// models in every lookup (CachedCatalog, CatalogFor, LoadCatalog,
// RefreshCatalog), so no test reaches the network or the disk cache. A nil or
// empty list means "no catalog". Call the returned func to restore. It lives
// outside a _test.go file because tests in other packages (tui, main) need it.
func SetCatalogForTest(provider string, models []CatalogModel) (restore func()) {
	catalogMu.Lock()
	prev, had := catalogOverride[provider]
	catalogOverride[provider] = models
	catalogMu.Unlock()
	return func() {
		catalogMu.Lock()
		defer catalogMu.Unlock()
		if had {
			catalogOverride[provider] = prev
		} else {
			delete(catalogOverride, provider)
		}
	}
}

// ForgetCatalogsForTest is for tests only: it drops every catalog loaded in
// this process, so a test that fetched one doesn't leak it into the next.
func ForgetCatalogsForTest() {
	waitCatalogRefreshes()
	resetCatalogMemory()
}

// SetCatalogFetchForTest is for tests only: it replaces the /models fetch,
// so tests in other packages can see what a fetch was asked (its context,
// endpoint) without the network. Call the returned func to restore.
func SetCatalogFetchForTest(fetch func(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, error)) (restore func()) {
	catalogMu.Lock()
	orig := catalogFetch
	catalogFetch = fetch
	catalogMu.Unlock()
	return func() {
		catalogMu.Lock()
		catalogFetch = orig
		catalogMu.Unlock()
		ForgetCatalogsForTest()
	}
}
