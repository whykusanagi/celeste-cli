package providers

import (
	"context"
	"testing"
)

func TestResolveModel(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name       string
		provider   string
		configured string
		cat        []CatalogModel
		ok         bool
		want       string
		wantNote   bool
	}{
		{
			name: "no catalog keeps the configured model", provider: "venice",
			configured: "venice-uncensored", want: "venice-uncensored",
		},
		{
			name: "no catalog and nothing configured uses the registry default", provider: "venice",
			want: "venice-uncensored-1-2",
		},
		{
			name: "an empty catalog counts as no catalog", provider: "openai",
			configured: "gpt-x", ok: true, want: "gpt-x",
		},
		{
			name: "a served model is kept", provider: "sakana", configured: "fugu-ultra", ok: true,
			cat:  []CatalogModel{{ID: "fugu"}, {ID: "fugu-ultra"}},
			want: "fugu-ultra",
		},
		{
			name: "an OpenRouter :variant of a served model is kept", provider: "openrouter",
			configured: "x/base:nitro", ok: true,
			cat:  []CatalogModel{{ID: "x/base"}},
			want: "x/base:nitro",
		},
		{
			name: "retired: the provider-flagged default wins", provider: "venice",
			configured: "venice-uncensored", ok: true,
			cat: []CatalogModel{
				{ID: "llama-3.3-70b", Tools: &yes},
				{ID: "venice-uncensored-1-1"},
				{ID: "zai-org-glm-5", Default: true, Tools: &yes},
			},
			want: "zai-org-glm-5", wantNote: true,
		},
		{
			name: "retired: among several flagged defaults a tool-capable one wins", provider: "venice",
			configured: "gone", ok: true,
			cat: []CatalogModel{
				{ID: "a", Default: true, Tools: &no},
				{ID: "b", Default: true, Tools: &yes},
			},
			want: "b", wantNote: true,
		},
		{
			name: "retired: the newest prefix match", provider: "venice",
			configured: "venice-uncensored", ok: true,
			cat: []CatalogModel{
				{ID: "llama-3.3-70b"},
				{ID: "venice-uncensored-1-1"},
				{ID: "venice-uncensored-1-2"},
				{ID: "venice-uncensoredish"}, // not a version of it
				{ID: "e2ee-venice-uncensored-24b-p"},
			},
			want: "venice-uncensored-1-2", wantNote: true,
		},
		{
			name: "retired: the prefix match accepts . and : separators", provider: "sakana",
			configured: "fugu-ultra-v1", ok: true,
			cat:  []CatalogModel{{ID: "fugu"}, {ID: "fugu-ultra-v1.1"}},
			want: "fugu-ultra-v1.1", wantNote: true,
		},
		{
			name: "retired: a tool-capable prefix match beats a newer tool-less one", provider: "venice",
			configured: "m", ok: true,
			cat:  []CatalogModel{{ID: "m-1", Tools: &yes}, {ID: "m-2", Tools: &no}},
			want: "m-1", wantNote: true,
		},
		{
			name: "retired: the registry default when it is served", provider: "sakana",
			configured: "old-model", ok: true,
			cat:  []CatalogModel{{ID: "fugu-ultra"}, {ID: "fugu"}},
			want: "fugu", wantNote: true,
		},
		{
			name: "retired: the first tool-capable model", provider: "openrouter",
			configured: "gone/model", ok: true,
			cat:  []CatalogModel{{ID: "a/x", Tools: &no}, {ID: "b/y", Tools: &yes}, {ID: "c/z", Tools: &yes}},
			want: "b/y", wantNote: true,
		},
		{
			name: "retired: else the first served model", provider: "openai",
			configured: "gone", ok: true,
			cat:  []CatalogModel{{ID: "first"}, {ID: "second"}},
			want: "first", wantNote: true,
		},
		{
			name: "nothing configured picks without a note", provider: "venice",
			ok:   true,
			cat:  []CatalogModel{{ID: "x"}, {ID: "venice-uncensored-1-2", Default: true}},
			want: "venice-uncensored-1-2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, note := ResolveModel(tt.provider, tt.configured, tt.cat, tt.ok)
			if got != tt.want {
				t.Errorf("model = %q, want %q", got, tt.want)
			}
			if (note != "") != tt.wantNote {
				t.Errorf("note = %q, want a note: %v", note, tt.wantNote)
			}
			if tt.wantNote {
				want := tt.provider + " no longer serves " + tt.configured + "; using " + tt.want
				if note != want {
					t.Errorf("note = %q, want %q", note, want)
				}
			}
		})
	}
}

func TestResolveEndpointModel(t *testing.T) {
	isolateCatalog(t)
	defer SetCatalogForTest("venice", []CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	model, note := ResolveEndpointModel(context.Background(), "https://api.venice.ai/api/v1", "k", "venice-uncensored")
	if model != "venice-uncensored-1-2" || note == "" {
		t.Errorf("got %q, %q", model, note)
	}
	// A provider without a catalog keeps the configured model.
	model, note = ResolveEndpointModel(context.Background(), "http://127.0.0.1:8080/v1", "", "/models/qwen")
	if model != "/models/qwen" || note != "" {
		t.Errorf("local: got %q, %q", model, note)
	}
}
