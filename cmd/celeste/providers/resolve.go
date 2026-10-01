package providers

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// ResolveModel picks the model a session uses from what the provider serves
// now, so a retired model is never sent. Pure; ok is false when there is no
// catalog (offline, no key, a provider without one).
//
//  1. No catalog: the configured model, unchanged (the registry default when
//     nothing is configured).
//  2. The configured model is served: keep it.
//  3. Otherwise, in order: the provider-flagged default; the newest served
//     version of the configured model (its ID plus "-", "." or ":" and more);
//     the registry default or preferred tool model, if served; the first
//     tool-capable served model; the first served model.
//
// Among ties (several flagged defaults or versions) a tool-capable model
// wins. The note says what happened; it is empty when nothing was replaced.
func ResolveModel(provider, configured string, cat []CatalogModel, ok bool) (model, note string) {
	if !ok || len(cat) == 0 {
		if configured == "" {
			return Registry[provider].DefaultModel, ""
		}
		return configured, ""
	}
	if _, served := findServed(cat, configured); served || neverReplace(provider, configured) {
		return configured, ""
	}
	// A "-latest" alias is rarely listed itself; while its family is served
	// the provider still answers it.
	if stem, isAlias := strings.CutSuffix(configured, "-latest"); isAlias && stem != "" {
		if _, served := findServed(cat, stem); served || len(versionsOf(cat, stem)) > 0 {
			return configured, ""
		}
	}
	return replaceModel(provider, configured, cat)
}

// replaceModel picks the replacement for a model known to be gone, with
// the note. With nothing safe listed the configured model is kept.
func replaceModel(provider, configured string, cat []CatalogModel) (model, note string) {
	model = pickServed(provider, configured, cat)
	if model == "" {
		// Nothing safe to switch to (only cost traps listed): keep it.
		if configured == "" {
			return Registry[provider].DefaultModel, ""
		}
		return configured, ""
	}
	if repl, deprecated := DeprecatedModels[strings.ToLower(model)]; deprecated {
		model = repl
	}
	if configured != "" {
		note = fmt.Sprintf("%s no longer serves %s; using %s", provider, configured, model)
	}
	return model, note
}

// neverReplace reports IDs celeste can't judge from a listing: OpenRouter
// presets ("@preset/..."), fine-tunes ("ft:..."), and anything path-like on
// a provider whose IDs are not vendor/model.
func neverReplace(provider, id string) bool {
	lower := strings.ToLower(id)
	return strings.HasPrefix(id, "@") || strings.HasPrefix(lower, "ft:") ||
		(provider != "openrouter" && strings.Contains(id, "/"))
}

// DeprecatedModels maps Grok models that xAI silently ROUTES to the
// cost-prohibitive grok-4.3 (the grok-4-1-* family) to a safe replacement
// (#51). config's reconcileModel migrates them; ResolveModel never picks
// one.
var DeprecatedModels = map[string]string{
	"grok-4-1-fast":               "grok-4.20-0309-non-reasoning",
	"grok-4-1-fast-reasoning":     "grok-4.20-0309-non-reasoning",
	"grok-4-1-fast-non-reasoning": "grok-4.20-0309-non-reasoning",
	"grok-4-1-reasoning":          "grok-4.20-0309-non-reasoning",
	"grok-4-1":                    "grok-4.20-0309-non-reasoning",
}

// costTrapPrefixes are model families a fallback must never land on: xAI
// bills grok-4.3 at a cost-prohibitive rate and routes grok-4-1-* to it
// (#51).
var costTrapPrefixes = []string{"grok-4.3", "grok-4-1"}

// costTrap reports a model a fallback must not pick.
func costTrap(id string) bool {
	lower := strings.ToLower(id)
	if _, deprecated := DeprecatedModels[lower]; deprecated {
		return true
	}
	for _, p := range costTrapPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// pickServed returns the replacement for a retired model, or "" when the
// catalog lists nothing safe.
func pickServed(provider, configured string, all []CatalogModel) string {
	var cat []CatalogModel
	for _, m := range all {
		if !costTrap(m.ID) {
			cat = append(cat, m)
		}
	}
	if len(cat) == 0 {
		return ""
	}
	var defaults []CatalogModel
	for _, m := range cat {
		if m.Default {
			defaults = append(defaults, m)
		}
	}
	if len(defaults) > 0 {
		return preferTools(provider, defaults)[0].ID
	}

	if configured != "" {
		if versions := versionsOf(cat, configured); len(versions) > 0 {
			best := ""
			for _, m := range preferTools(provider, versions) {
				if best == "" || compareVersions(m.ID[len(configured):], best[len(configured):]) > 0 {
					best = m.ID
				}
			}
			return best
		}
	}

	caps := Registry[provider]
	for _, id := range []string{caps.DefaultModel, caps.PreferredToolModel} {
		if m, served := findServed(cat, id); served {
			return m.ID
		}
	}

	return preferTools(provider, cat)[0].ID
}

// preferTools returns the tool-capable models if there are any, else all of
// them, keeping their order. Where the catalog doesn't say, the provider's
// name heuristic decides, which also skips embedding, image and audio models
// in a generic /models listing.
func preferTools(provider string, models []CatalogModel) []CatalogModel {
	detect := NewModelDetection(provider)
	var tools []CatalogModel
	for _, m := range models {
		if m.Tools != nil && *m.Tools || m.Tools == nil && detect.SupportsTools(m.ID) {
			tools = append(tools, m)
		}
	}
	if len(tools) > 0 {
		return tools
	}
	return models
}

// versionSuffix matches what may follow a model ID in a newer version of it:
// a separator, then only version or date parts ("-1-2", ".1", "-20250929",
// "-v1.1"). A sibling such as gpt-4o-mini is not a version of gpt-4o.
var versionSuffix = regexp.MustCompile(`^[-.:]v?\d+([-._:]v?\d+)*$`)

// versionsOf returns the served models that are versions of id (case
// ignored).
func versionsOf(cat []CatalogModel, id string) []CatalogModel {
	var out []CatalogModel
	for _, m := range cat {
		if len(m.ID) > len(id) && strings.EqualFold(m.ID[:len(id)], id) && versionSuffix.MatchString(m.ID[len(id):]) {
			out = append(out, m)
		}
	}
	return out
}

// compareVersions compares two version suffixes: version numbers first
// (v1.10 is newer than v1.9), then the date, so "-4-6" beats "-1-20250805"
// and both beat a bare "-20250514".
func compareVersions(a, b string) int {
	va, da := versionNumbers(a)
	vb, db := versionNumbers(b)
	for i := 0; i < len(va) && i < len(vb); i++ {
		if c := va[i].Cmp(vb[i]); c != 0 {
			return c
		}
	}
	if len(va) != len(vb) {
		return len(va) - len(vb)
	}
	return strings.Compare(da, db)
}

var digitRuns = regexp.MustCompile(`\d+`)

// versionNumbers splits a suffix into its version numbers and its date
// (an 8-digit run, YYYYMMDD).
func versionNumbers(s string) (version []*big.Int, date string) {
	for _, part := range digitRuns.FindAllString(s, -1) {
		if len(part) == 8 {
			date = part
			continue
		}
		n, _ := new(big.Int).SetString(part, 10)
		version = append(version, n)
	}
	return version, date
}
