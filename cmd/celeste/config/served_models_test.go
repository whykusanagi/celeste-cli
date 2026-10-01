package config

import (
	"context"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

func TestResolveServedModels(t *testing.T) {
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{
		{ID: "venice-uncensored-1-2", Default: true},
		{ID: "llama-3.3-70b"},
	})()
	c := &Config{BaseURL: "https://api.venice.ai/api/v1", APIKey: "k", Model: "venice-uncensored", AgentModel: "llama-3.3-70b"}
	notes := c.ResolveServedModels(context.Background())
	if c.Model != "venice-uncensored-1-2" || c.AgentModel != "llama-3.3-70b" {
		t.Errorf("model=%q agent=%q", c.Model, c.AgentModel)
	}
	if len(notes) != 1 || notes[0] != "venice no longer serves venice-uncensored; using venice-uncensored-1-2" {
		t.Errorf("notes = %q", notes)
	}
}

func TestResolveServedModels_RetiredAgentModel(t *testing.T) {
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	c := &Config{BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored-1-2", AgentModel: "gone", SmallModel: "also-gone"}
	notes := c.ResolveServedModels(context.Background())
	if c.AgentModel != "venice-uncensored-1-2" || c.SmallModel != "venice-uncensored-1-2" || len(notes) != 2 {
		t.Errorf("agent=%q small=%q notes=%q", c.AgentModel, c.SmallModel, notes)
	}
}
