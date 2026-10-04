package commands

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// V5: a --force model nothing checked is marked unverified, so the header
// does not show ✓ for it. A served model set with --force is verified.
func TestSetModelForceMarksUnverifiedModels(t *testing.T) {
	veniceCatalogForTest(t)
	ctx := &CommandContext{Provider: "venice"}

	r := Execute(&Command{Name: "set-model", Args: []string{"my-private-model", "--force"}}, ctx)
	if !r.Success || r.StateChange == nil || !r.StateChange.ModelUnverified {
		t.Errorf("a forced model outside the catalog must be unverified: %+v", r)
	}
	r = Execute(&Command{Name: "set-model", Args: []string{"venice-uncensored-1-2", "--force"}}, ctx)
	if !r.Success || r.StateChange.ModelUnverified {
		t.Errorf("a served model is verified even with --force: %+v", r.StateChange)
	}
	r = Execute(&Command{Name: "set-model", Args: []string{"venice-uncensored-1-2"}}, ctx)
	if r.StateChange.ModelUnverified {
		t.Errorf("a served model is verified: %+v", r.StateChange)
	}

	// No catalog at all: --force pins a name nothing will ever check.
	t.Cleanup(providers.SetCatalogForTest("sakana", nil))
	r = Execute(&Command{Name: "set-model", Args: []string{"fugu-made-up", "--force"}}, &CommandContext{Provider: "sakana"})
	if !r.Success || !r.StateChange.ModelUnverified {
		t.Errorf("a forced model with no catalog must be unverified: %+v", r.StateChange)
	}
}
