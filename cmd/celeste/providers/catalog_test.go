package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Trimmed captures of each /models shape celeste reads.
const (
	veniceModelsFixture = `{"object":"list","type":"text","data":[
	 {"id":"venice-uncensored-1-2","type":"text","model_spec":{"traits":["default","most_uncensored"],"capabilities":{"supportsFunctionCalling":true}}},
	 {"id":"e2ee-venice-uncensored-24b-p","type":"text","model_spec":{"traits":[],"capabilities":{"supportsFunctionCalling":false}}},
	 {"id":"zai-org-glm-5","type":"text","model_spec":{"traits":["default_code"],"capabilities":{"supportsFunctionCalling":true}}},
	 {"id":"some-image-model","type":"image","model_spec":{"traits":["default"]}}
	]}`
	openRouterModelsFixture = `{"data":[
	 {"id":"openai/gpt-4o","supported_parameters":["tools","temperature","max_tokens"]},
	 {"id":"meta-llama/llama-3-8b-instruct","supported_parameters":["temperature","top_p"]},
	 {"id":"some/no-params"}
	]}`
	openAIModelsFixture = `{"object":"list","data":[
	 {"id":"fugu","object":"model","owned_by":"sakana"},
	 {"id":"fugu-ultra","object":"model","owned_by":"sakana"}
	]}`
	anthropicModelsFixture = `{"data":[
	 {"type":"model","id":"claude-sonnet-4-5-20250929","display_name":"Claude Sonnet 4.5","created_at":"2025-09-29T00:00:00Z"},
	 {"type":"model","id":"claude-opus-4-5-20251101","display_name":"Claude Opus 4.5","created_at":"2025-11-01T00:00:00Z"}
	],"has_more":false,"first_id":"claude-sonnet-4-5-20250929","last_id":"claude-opus-4-5-20251101"}`
)

func findModel(cat []CatalogModel, id string) (CatalogModel, bool) {
	for _, m := range cat {
		if m.ID == id {
			return m, true
		}
	}
	return CatalogModel{}, false
}

func TestParseCatalog_Venice(t *testing.T) {
	cat, err := parseCatalog("venice", []byte(veniceModelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findModel(cat, "some-image-model"); ok {
		t.Error("an image model must not be in the chat catalog")
	}
	m, ok := findModel(cat, "venice-uncensored-1-2")
	if !ok || !m.Default || m.Tools == nil || !*m.Tools {
		t.Errorf("venice-uncensored-1-2 = %+v, want default with tools", m)
	}
	m, _ = findModel(cat, "e2ee-venice-uncensored-24b-p")
	if m.Default || m.Tools == nil || *m.Tools {
		t.Errorf("e2ee model = %+v, want no default, tools false", m)
	}
	if m, _ := findModel(cat, "zai-org-glm-5"); m.Default {
		t.Error("a default_code trait is not the default model")
	}
}

func TestParseCatalog_OpenRouter(t *testing.T) {
	cat, err := parseCatalog("openrouter", []byte(openRouterModelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"openai/gpt-4o": true, "meta-llama/llama-3-8b-instruct": false, "some/no-params": false} {
		m, ok := findModel(cat, id)
		if !ok || m.Tools == nil || *m.Tools != want {
			t.Errorf("%s = %+v, want tools %v", id, m, want)
		}
	}
}

func TestParseCatalog_OpenAICompatible(t *testing.T) {
	cat, err := parseCatalog("sakana", []byte(openAIModelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 2 || cat[0].ID != "fugu" || cat[0].Tools != nil || cat[0].Default {
		t.Errorf("catalog = %+v, want fugu and fugu-ultra with unknown tools", cat)
	}
}

func TestParseCatalog_Anthropic(t *testing.T) {
	cat, err := parseCatalog("anthropic", []byte(anthropicModelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 2 || cat[1].ID != "claude-opus-4-5-20251101" {
		t.Errorf("catalog = %+v", cat)
	}
}

func TestParseCatalog_BadJSON(t *testing.T) {
	if _, err := parseCatalog("openai", []byte("nope")); err == nil {
		t.Error("bad JSON must be an error")
	}
	if _, err := parseCatalog("openai", []byte(`{"data":[]}`)); err == nil {
		t.Error("an empty catalog must be an error, not an empty answer")
	}
}

// isolateCatalog points the cache at a temp dir and clears the in-memory
// state, so a test sees neither the real home nor another test's catalog.
func isolateCatalog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origDir, origNow, origFetch, origVerify := catalogDir, catalogNow, catalogFetch, catalogVerify
	catalogDir = func() string { return dir }
	catalogVerify = func(context.Context, string, string, string, string) (bool, bool) { return false, false }
	resetCatalogMemory()
	t.Cleanup(func() {
		catalogDir, catalogNow, catalogFetch, catalogVerify = origDir, origNow, origFetch, origVerify
		waitCatalogRefreshes()
		resetCatalogMemory()
	})
	return dir
}

func TestFetchCatalog_SendsAuthAndParses(t *testing.T) {
	isolateCatalog(t)
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(openAIModelsFixture))
	}))
	defer srv.Close()

	cat, err := fetchCatalog(context.Background(), "sakana", srv.URL+"/v1", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-key" || gotPath != "/v1/models" {
		t.Errorf("auth=%q path=%q", gotAuth, gotPath)
	}
	if len(cat) != 2 {
		t.Errorf("catalog = %+v", cat)
	}
}

func TestFetchCatalog_AnthropicHeaders(t *testing.T) {
	isolateCatalog(t)
	var key, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, version = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(anthropicModelsFixture))
	}))
	defer srv.Close()

	if _, err := fetchCatalog(context.Background(), "anthropic", srv.URL+"/v1", "secret-key"); err != nil {
		t.Fatal(err)
	}
	if key != "secret-key" || version != "2023-06-01" {
		t.Errorf("x-api-key=%q anthropic-version=%q", key, version)
	}
}

func TestFetchCatalog_ErrorNeverContainsKey(t *testing.T) {
	isolateCatalog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := fetchCatalog(context.Background(), "openai", srv.URL, "secret-key")
	if err == nil {
		t.Fatal("a 401 must be an error")
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Errorf("error leaks the key: %v", err)
	}
}

func TestFetchCatalog_NoCatalogProvider(t *testing.T) {
	isolateCatalog(t)
	for _, p := range []string{"gemini", "vertex", "local", "digitalocean", "elevenlabs", "unknown"} {
		if _, err := fetchCatalog(context.Background(), p, "http://127.0.0.1:1", "k"); err == nil {
			t.Errorf("%s has no catalog; fetch must fail without a request", p)
		}
	}
}

func stubFetch(t *testing.T, models []CatalogModel, err error) *int32 {
	t.Helper()
	var calls int32
	catalogFetch = func(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, error) {
		atomic.AddInt32(&calls, 1)
		return models, err
	}
	return &calls
}

func TestLoadCatalog_FetchesOnceAndCachesToDisk(t *testing.T) {
	dir := isolateCatalog(t)
	calls := stubFetch(t, []CatalogModel{{ID: "fugu"}}, nil)

	cat, ok := LoadCatalog(context.Background(), "sakana", "https://api.sakana.ai/v1", "secret-key")
	if !ok || len(cat) != 1 || cat[0].ID != "fugu" {
		t.Fatalf("LoadCatalog = %+v, %v", cat, ok)
	}
	if _, ok := LoadCatalog(context.Background(), "sakana", "https://api.sakana.ai/v1", "secret-key"); !ok || atomic.LoadInt32(calls) != 1 {
		t.Errorf("second load fetched again (%d calls)", atomic.LoadInt32(calls))
	}

	files, _ := filepath.Glob(filepath.Join(dir, "sakana-*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files = %v, want one sakana-<hash>.json", files)
	}
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "secret-key") {
		t.Error("the cache file contains the API key")
	}
	if !strings.Contains(string(data), `"fetched_at"`) || !strings.Contains(string(data), `"base_url"`) {
		t.Errorf("cache file = %s", data)
	}
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm() != 0o700 && os.PathSeparator == '/' {
		t.Errorf("cache dir mode = %v, want 0700", fi.Mode().Perm())
	}

	// A new process (empty memory) reads the disk cache without fetching.
	resetCatalogMemory()
	cat, stale, ok := CachedCatalog("sakana", "https://api.sakana.ai/v1", "secret-key")
	if !ok || stale || cat[0].ID != "fugu" {
		t.Errorf("CachedCatalog after restart = %+v stale=%v ok=%v", cat, stale, ok)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Error("reading the disk cache must not fetch")
	}
}

func TestLoadCatalog_StaleCacheIsUsedAndRefreshed(t *testing.T) {
	isolateCatalog(t)
	start := time.Now()
	catalogNow = func() time.Time { return start }
	stubFetch(t, []CatalogModel{{ID: "old"}}, nil)
	LoadCatalog(context.Background(), "openai", "", "k")

	catalogNow = func() time.Time { return start.Add(25 * time.Hour) }
	calls := stubFetch(t, []CatalogModel{{ID: "new"}}, nil)
	cat, ok := LoadCatalog(context.Background(), "openai", "", "k")
	if !ok || cat[0].ID != "old" {
		t.Fatalf("a stale cache must still answer at once, got %+v", cat)
	}
	waitCatalogRefreshes()
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("stale cache started %d refreshes, want 1", atomic.LoadInt32(calls))
	}
	cat, stale, _ := CachedCatalog("openai", "", "k")
	if cat[0].ID != "new" || stale {
		t.Errorf("after the background refresh: %+v stale=%v", cat, stale)
	}
}

func TestRefreshCatalog_FailureKeepsOldCache(t *testing.T) {
	dir := isolateCatalog(t)
	stubFetch(t, []CatalogModel{{ID: "good"}}, nil)
	LoadCatalog(context.Background(), "openai", "", "k")

	stubFetch(t, nil, errors.New("offline"))
	if _, err := RefreshCatalog(context.Background(), "openai", "", "k"); err == nil {
		t.Fatal("want the fetch error")
	}
	resetCatalogMemory()
	cat, _, ok := CachedCatalog("openai", "", "k")
	if !ok || cat[0].ID != "good" {
		t.Errorf("a failed fetch lost the cache: %+v %v", cat, ok)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 {
		t.Errorf("cache dir = %v, want only the one cache file", files)
	}
}

func TestCachedCatalog_CorruptFileIgnored(t *testing.T) {
	isolateCatalog(t)
	path := catalogCachePath("openai", "https://api.openai.com/v1", "k")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := CachedCatalog("openai", "", "k"); ok {
		t.Error("a corrupt cache file must read as no cache")
	}
}

func TestLoadCatalog_NoCatalogProviderNeverFetches(t *testing.T) {
	isolateCatalog(t)
	calls := stubFetch(t, []CatalogModel{{ID: "x"}}, nil)
	if _, ok := LoadCatalog(context.Background(), "local", "http://127.0.0.1:8080/v1", ""); ok {
		t.Error("local has no catalog")
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Error("a provider without a catalog must not fetch")
	}
}

func TestCatalogFor_LatestLoadedForProvider(t *testing.T) {
	isolateCatalog(t)
	if _, ok := CatalogFor("venice"); ok {
		t.Fatal("nothing loaded yet")
	}
	stubFetch(t, []CatalogModel{{ID: "venice-uncensored-1-2"}}, nil)
	LoadCatalog(context.Background(), "venice", "", "k")
	if cat, ok := CatalogFor("venice"); !ok || cat[0].ID != "venice-uncensored-1-2" {
		t.Errorf("CatalogFor = %+v, %v", cat, ok)
	}
}

func TestSetCatalogForTest(t *testing.T) {
	isolateCatalog(t)
	calls := stubFetch(t, nil, errors.New("must not fetch"))
	restore := SetCatalogForTest("venice", []CatalogModel{{ID: "a"}})
	if cat, ok := LoadCatalog(context.Background(), "venice", "", "k"); !ok || cat[0].ID != "a" {
		t.Errorf("LoadCatalog under the test catalog = %+v %v", cat, ok)
	}
	if cat, ok := CatalogFor("venice"); !ok || cat[0].ID != "a" {
		t.Errorf("CatalogFor under the test catalog = %+v %v", cat, ok)
	}
	restore()
	if _, ok := CatalogFor("venice"); ok {
		t.Error("restore must remove the test catalog")
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Error("the test catalog must stop network fetches")
	}
}

// The Anthropic listing is GET {host}/v1/models whether the configured base
// URL is the bare host (the registry form, what the SDK wants for chat) or
// ends in /v1 (what celeste advertised before 2.0).
func TestFetchCatalog_AnthropicPathForBothBaseURLForms(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			isolateCatalog(t)
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(anthropicModelsFixture))
			}))
			defer srv.Close()

			if _, err := fetchCatalog(context.Background(), "anthropic", srv.URL+suffix, "k"); err != nil {
				t.Fatal(err)
			}
			if gotPath != "/v1/models" {
				t.Errorf("listing path = %q, want /v1/models", gotPath)
			}
		})
	}
}

func TestVerifyModel_AnthropicPathForBothBaseURLForms(t *testing.T) {
	for _, suffix := range []string{"", "/v1"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(`{"id":"claude-x"}`))
			}))
			defer srv.Close()

			served, known := verifyModel(context.Background(), "anthropic", srv.URL+suffix, "k", "claude-x")
			if !served || !known || gotPath != "/v1/models/claude-x" {
				t.Errorf("served=%v known=%v path=%q", served, known, gotPath)
			}
		})
	}
}

// Both forms are one endpoint, so they share one catalog cache entry.
func TestNormalizeBaseURL_AnthropicFormsShareOneKey(t *testing.T) {
	bare := normalizeBaseURL("anthropic", "https://api.anthropic.com")
	for _, in := range []string{"", "https://api.anthropic.com/", "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/"} {
		if got := normalizeBaseURL("anthropic", in); got != bare {
			t.Errorf("normalizeBaseURL(anthropic, %q) = %q, want %q", in, got, bare)
		}
	}
	// Other providers keep their /v1: it is part of their API root.
	if got := normalizeBaseURL("openai", "https://api.openai.com/v1"); got != "https://api.openai.com/v1" {
		t.Errorf("openai base = %q", got)
	}
}
