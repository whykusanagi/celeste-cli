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

// A false "no longer serves" on a working model is a bug: these are the
// cases the review measured.
func TestResolveModel_NeverReplacesWorkingIDs(t *testing.T) {
	tests := []struct {
		name, provider, configured string
		cat                        []CatalogModel
	}{
		{"case-insensitive match", "openrouter", "Anthropic/Claude-Sonnet-4.5",
			[]CatalogModel{{ID: "anthropic/claude-sonnet-4.5"}, {ID: "openai/gpt-4.1-nano"}}},
		{"OpenRouter preset", "openrouter", "@preset/mine",
			[]CatalogModel{{ID: "openai/gpt-4.1-nano"}}},
		{"fine-tune", "openai", "ft:gpt-4.1-nano:acme::abc123",
			[]CatalogModel{{ID: "gpt-4.1-nano"}}},
		{"a path-like ID off OpenRouter", "sakana", "org/custom-model",
			[]CatalogModel{{ID: "fugu"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, note := ResolveModel(tt.provider, tt.configured, tt.cat, true)
			if got != tt.configured || note != "" {
				t.Errorf("got %q (%q), want %q kept", got, note, tt.configured)
			}
		})
	}
}

// #51: a Grok fallback must never land on the grok-4-1-* family or grok-4.3.
func TestResolveModel_GrokCostTrap(t *testing.T) {
	cat := []CatalogModel{{ID: "grok-4-1-fast-reasoning"}, {ID: "grok-4.3"}, {ID: "grok-4-1"}, {ID: "grok-build-0.1"}, {ID: "grok-imagine-image"}}
	got, _ := ResolveModel("grok", "grok-4.20-0309-non-reasoning", cat, true)
	if got != "grok-build-0.1" {
		t.Errorf("retired default -> %q, want grok-build-0.1", got)
	}
	got, _ = ResolveModel("grok", "grok-4", []CatalogModel{{ID: "grok-4.3"}, {ID: "grok-4-1-fast"}, {ID: "grok-build-0.1"}}, true)
	if got != "grok-build-0.1" {
		t.Errorf("grok-4 -> %q, must not pick grok-4.3 or grok-4-1-*", got)
	}
	// Nothing safe left: keep the configured model rather than pay grok-4.3.
	got, note := ResolveModel("grok", "grok-old", []CatalogModel{{ID: "grok-4.3"}, {ID: "grok-4-1-fast"}}, true)
	if got != "grok-old" || note != "" {
		t.Errorf("only cost traps listed -> %q (%q), want the configured model kept", got, note)
	}
}

// M1: dates are compared after the version parts, so claude-opus-4-6 beats a
// dated claude-opus-4 snapshot and claude-opus-4-1-*.
func TestResolveModel_VersionBeforeDate(t *testing.T) {
	cat := []CatalogModel{{ID: "claude-opus-4-20250514"}, {ID: "claude-opus-4-1-20250805"}, {ID: "claude-opus-4-6"}}
	if got, _ := ResolveModel("anthropic", "claude-opus-4", cat, true); got != "claude-opus-4-6" {
		t.Errorf("got %q, want claude-opus-4-6", got)
	}
	cat = []CatalogModel{{ID: "m-20250101"}, {ID: "m-20250601"}}
	if got, _ := ResolveModel("openai", "m", cat, true); got != "m-20250601" {
		t.Errorf("got %q, want the newer date", got)
	}
}

// M3: a ":variant" falls back to its base only when no variant of the base
// is listed at all.
func TestFindServed_VariantFallback(t *testing.T) {
	cat := []CatalogModel{{ID: "x/base"}, {ID: "x/base:nitro"}}
	if _, ok := findServed(cat, "x/base:free"); ok {
		t.Error("x/base:free is retired while other variants are listed")
	}
	if _, ok := findServed([]CatalogModel{{ID: "x/base"}}, "x/base:floor"); !ok {
		t.Error("a routing suffix on a listed base with no listed variants is served")
	}
}

// isolateEndpoints is isolateCatalog plus stubbed verification.
func stubVerify(t *testing.T, fn func(id string) (served, known bool)) *int32 {
	t.Helper()
	var calls int32
	orig := catalogVerify
	catalogVerify = func(ctx context.Context, provider, baseURL, apiKey, id string) (bool, bool) {
		atomic.AddInt32(&calls, 1)
		return fn(id)
	}
	t.Cleanup(func() { catalogVerify = orig })
	return &calls
}

// 1(c): on a catalog miss, providers with GET /models/{id} are asked before
// anything is replaced: 200 keeps, 404 replaces, anything else keeps.
func TestResolveFromMemory_VerifiesMissesBeforeReplacing(t *testing.T) {
	isolateCatalog(t)
	stubFetch(t, []CatalogModel{{ID: "claude-sonnet-4-5-20250929"}, {ID: "claude-opus-4-5-20251101"}}, nil)
	calls := stubVerify(t, func(id string) (bool, bool) {
		switch id {
		case "claude-opus-4-0", "claude-sonnet-4-5":
			return true, true // aliases the API answers
		case "claude-2":
			return false, true // 404
		}
		return false, false // 500 / timeout
	})
	ctx := context.Background()
	base, key := "https://api.anthropic.com/v1", "k"

	// Before Prepare, a miss is pending, never replaced.
	if got, note, pending := ResolveFromMemory("anthropic", base, key, "claude-opus-4-0"); got != "claude-opus-4-0" || note != "" || !pending {
		t.Errorf("before prepare: %q %q pending=%v", got, note, pending)
	}

	PrepareModels(ctx, "anthropic", base, key, "claude-opus-4-0", "claude-sonnet-4-5", "claude-2", "claude-flaky")
	for _, alias := range []string{"claude-opus-4-0", "claude-sonnet-4-5"} {
		if got, note, _ := ResolveFromMemory("anthropic", base, key, alias); got != alias || note != "" {
			t.Errorf("%s -> %q (%q), want kept", alias, got, note)
		}
	}
	if got, note, _ := ResolveFromMemory("anthropic", base, key, "claude-2"); got == "claude-2" || note == "" {
		t.Errorf("a 404 model must be replaced, got %q", got)
	}
	if got, note, pending := ResolveFromMemory("anthropic", base, key, "claude-flaky"); got != "claude-flaky" || note != "" || pending {
		t.Errorf("an unknown answer keeps the model and is not retried at once: %q %q pending=%v", got, note, pending)
	}
	n := atomic.LoadInt32(calls)
	PrepareModels(ctx, "anthropic", base, key, "claude-opus-4-0", "claude-2", "claude-flaky")
	if atomic.LoadInt32(calls) != n {
		t.Error("verification results must be remembered (and failures negative-cached)")
	}
}

// Providers without a per-model endpoint replace on a miss.
func TestResolveFromMemory_NoVerifyProviderReplacesOnMiss(t *testing.T) {
	isolateCatalog(t)
	stubFetch(t, []CatalogModel{{ID: "venice-uncensored-1-2", Default: true}}, nil)
	calls := stubVerify(t, func(string) (bool, bool) { return true, true })
	PrepareModels(context.Background(), "venice", "", "k", "venice-uncensored")
	if got, _, _ := ResolveFromMemory("venice", "", "k", "venice-uncensored"); got != "venice-uncensored-1-2" {
		t.Errorf("got %q", got)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Error("venice has no per-model endpoint")
	}
}

func TestVerifyModel_HTTP(t *testing.T) {
	isolateCatalog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/alive":
			_, _ = w.Write([]byte(`{"id":"alive"}`))
		case "/v1/models/gone":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	for id, want := range map[string][2]bool{"alive": {true, true}, "gone": {false, true}, "boom": {false, false}} {
		served, known := verifyModel(context.Background(), "openai", srv.URL+"/v1", "k", id)
		if served != want[0] || known != want[1] {
			t.Errorf("%s: served=%v known=%v, want %v", id, served, known, want)
		}
	}
}

// 4: two keys on one endpoint never share a catalog, in memory or on disk.
func TestCatalog_SeparatedByAPIKey(t *testing.T) {
	dir := isolateCatalog(t)
	stubFetch(t, []CatalogModel{{ID: "model-a"}}, nil)
	LoadCatalog(context.Background(), "openai", "", "key-a")
	if _, _, ok := CachedCatalog("openai", "", "key-b"); ok {
		t.Error("key-b sees key-a's catalog")
	}
	stubFetch(t, []CatalogModel{{ID: "model-b"}}, nil)
	LoadCatalog(context.Background(), "openai", "", "key-b")
	files, _ := filepath.Glob(filepath.Join(dir, "openai-*.json"))
	if len(files) != 2 {
		t.Fatalf("cache files = %v, want one per key", files)
	}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), "key-a") || strings.Contains(string(data), "key-b") || strings.Contains(f, "key-") {
			t.Errorf("%s leaks a key", f)
		}
	}
}

// 7: a failed fetch is not retried for five minutes.
func TestLoadCatalog_NegativeCache(t *testing.T) {
	isolateCatalog(t)
	start := time.Now()
	catalogNow = func() time.Time { return start }
	calls := stubFetch(t, nil, errors.New("offline"))
	for i := 0; i < 3; i++ {
		if _, ok := LoadCatalog(context.Background(), "openai", "", "k"); ok {
			t.Fatal("no catalog expected")
		}
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("fetches = %d, want 1 within the negative-cache window", atomic.LoadInt32(calls))
	}
	if _, _, pending := ResolveFromMemory("openai", "", "k", "gpt-x"); pending {
		t.Error("a negative-cached endpoint is not pending")
	}
	catalogNow = func() time.Time { return start.Add(6 * time.Minute) }
	LoadCatalog(context.Background(), "openai", "", "k")
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("fetches = %d, want a retry after the window", atomic.LoadInt32(calls))
	}
}

// M2: Anthropic's listing paginates.
func TestFetchCatalog_AnthropicPagination(t *testing.T) {
	isolateCatalog(t)
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b"}],"has_more":false,"last_id":"b"}`))
	}))
	defer srv.Close()
	cat, err := fetchCatalog(context.Background(), "anthropic", srv.URL+"/v1", "k")
	if err != nil || len(cat) != 2 {
		t.Fatalf("%+v %v", cat, err)
	}
	if !strings.Contains(queries[0], "limit=1000") || !strings.Contains(queries[1], "after_id=a") {
		t.Errorf("queries = %v", queries)
	}
}

// M6: credentials and queries in a base URL never reach the cache file.
func TestCatalog_StripsUserinfoAndQuery(t *testing.T) {
	dir := isolateCatalog(t)
	var gotPath string
	catalogFetch = func(ctx context.Context, provider, baseURL, apiKey string) ([]CatalogModel, error) {
		gotPath = baseURL
		return []CatalogModel{{ID: "m"}}, nil
	}
	LoadCatalog(context.Background(), "openai", "https://user:secret@api.example.com/v1/?token=t0ps3cret", "k")
	if strings.Contains(gotPath, "secret") || strings.Contains(gotPath, "t0ps3cret") {
		t.Errorf("fetch base = %q", gotPath)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "t0ps3cret") {
		t.Errorf("cache file = %s", data)
	}
}

// M7: a temp file left by a crashed write is swept.
func TestWriteCatalogFile_SweepsStaleTemps(t *testing.T) {
	dir := isolateCatalog(t)
	old := filepath.Join(dir, ".openai-x.json.tmp-123")
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(old, past, past)
	fresh := filepath.Join(dir, ".openai-y.json.tmp-456")
	_ = os.WriteFile(fresh, []byte("y"), 0o600)
	stubFetch(t, []CatalogModel{{ID: "m"}}, nil)
	LoadCatalog(context.Background(), "openai", "", "k")
	if _, err := os.Stat(old); err == nil {
		t.Error("a stale temp file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh temp file (a write in progress) was removed")
	}
}

// A -latest alias the provider answered 404 for is gone, even while its
// family is listed.
func TestResolveFromMemory_VerifiedGoneLatestIsReplaced(t *testing.T) {
	isolateCatalog(t)
	stubFetch(t, []CatalogModel{{ID: "gpt-5-20260101"}, {ID: "gpt-4.1-nano"}}, nil)
	stubVerify(t, func(string) (bool, bool) { return false, true })
	PrepareModels(context.Background(), "openai", "", "k", "gpt-5-latest")
	if got, note, _ := ResolveFromMemory("openai", "", "k", "gpt-5-latest"); got == "gpt-5-latest" || note == "" {
		t.Errorf("a 404 -latest alias was kept")
	}
}

func TestCleanBaseURL(t *testing.T) {
	if got := CleanBaseURL("https://u:p4ss@h.example/v1?k=s3cret"); strings.Contains(got, "p4ss") || strings.Contains(got, "s3cret") {
		t.Errorf("CleanBaseURL = %q", got)
	}
}
