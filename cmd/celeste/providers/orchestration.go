package providers

import "strings"

// OrchestratesServerSide reports whether the model does its own task
// decomposition, delegation and verification server-side, which makes
// celeste-cli's local planning phase redundant. Fugu is a trained conductor:
// it either answers directly or coordinates several frontier models and
// synthesises the result. Running our planner on top of it means two planners
// that cannot see each other.
//
// This is deliberately a rule rather than a lookup against the static model
// list. Sakana ships version-bumped fugu IDs faster than that list is updated,
// and an unrecognised ID must not silently re-enable local planning against a
// conductor. The provider is checked too: Sakana serves an OpenAI-compatible
// API and could host a plain model, which still needs local planning.
func OrchestratesServerSide(provider, modelID string) bool {
	return provider == "sakana" && strings.HasPrefix(modelID, "fugu")
}
