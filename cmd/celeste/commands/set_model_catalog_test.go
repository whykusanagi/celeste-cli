package commands

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

func veniceCatalogForTest(t *testing.T) {
	yes, no := true, false
	t.Cleanup(providers.SetCatalogForTest("venice", []providers.CatalogModel{
		{ID: "venice-uncensored-1-2", Default: true, Tools: &yes},
		{ID: "e2ee-venice-uncensored-24b-p", Tools: &no},
	}))
}

// /set-model validates against the cached catalog; it never fetches.
func TestSetModel_ValidatesAgainstCatalog(t *testing.T) {
	veniceCatalogForTest(t)
	ctx := &CommandContext{Provider: "venice"}

	r := Execute(&Command{Name: "set-model", Args: []string{"venice-uncensored-1-2"}}, ctx)
	if !r.Success || r.StateChange == nil || *r.StateChange.Model != "venice-uncensored-1-2" {
		t.Errorf("a served model must be accepted: %+v", r)
	}

	r = Execute(&Command{Name: "set-model", Args: []string{"venice-uncensored"}}, ctx)
	if r.Success || !strings.Contains(r.Message, "not found") {
		t.Errorf("a retired model must be refused: %+v", r)
	}

	r = Execute(&Command{Name: "set-model", Args: []string{"venice-uncensored", "--force"}}, ctx)
	if !r.Success || *r.StateChange.Model != "venice-uncensored" || !r.StateChange.PinModel {
		t.Errorf("--force must still set it, pinned: %+v", r)
	}

	ctx.SkillsEnabled = true
	r = Execute(&Command{Name: "set-model", Args: []string{"e2ee-venice-uncensored-24b-p"}}, ctx)
	if r.Success {
		t.Errorf("a tool-less model with skills on needs --force: %+v", r)
	}
}

// With no catalog loaded the model is accepted, unvalidated, as before.
func TestSetModel_NoCatalogAccepts(t *testing.T) {
	t.Cleanup(providers.SetCatalogForTest("sakana", nil))
	r := Execute(&Command{Name: "set-model", Args: []string{"fugu-ultra"}}, &CommandContext{Provider: "sakana"})
	if !r.Success || *r.StateChange.Model != "fugu-ultra" {
		t.Errorf("%+v", r)
	}
}

// The picker lists the catalog when one is loaded.
func TestListModels_UsesCatalog(t *testing.T) {
	veniceCatalogForTest(t)
	r := Execute(&Command{Name: "list-models"}, &CommandContext{Provider: "venice"})
	if r.StateChange == nil || r.StateChange.ShowSelector == nil {
		t.Fatalf("%+v", r)
	}
	var ids []string
	for _, it := range r.StateChange.ShowSelector.Items {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "venice-uncensored-1-2,e2ee-venice-uncensored-24b-p" {
		t.Errorf("items = %v", ids)
	}
}

// IDs match case-insensitively, and on providers that answer GET
// /models/{id} an unlisted name (an alias) is accepted for the chat to
// check rather than refused.
func TestSetModel_CaseAndUnlistedAliases(t *testing.T) {
	veniceCatalogForTest(t)
	r := Execute(&Command{Name: "set-model", Args: []string{"Venice-Uncensored-1-2"}}, &CommandContext{Provider: "venice"})
	if !r.Success {
		t.Errorf("case must not matter: %+v", r)
	}
	t.Cleanup(providers.SetCatalogForTest("anthropic", []providers.CatalogModel{{ID: "claude-sonnet-4-5-20250929"}}))
	r = Execute(&Command{Name: "set-model", Args: []string{"claude-sonnet-4-5"}}, &CommandContext{Provider: "anthropic"})
	if !r.Success || r.StateChange.PinModel {
		t.Errorf("an unlisted alias on Anthropic is accepted, unpinned: %+v", r)
	}
}
