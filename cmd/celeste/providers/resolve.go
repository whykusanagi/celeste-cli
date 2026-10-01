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
	if _, served := findServed(cat, configured); served {
		return configured, ""
	}
	// A "-latest" alias is rarely listed itself; while its family is served
	// the provider still answers it.
	if stem, isAlias := strings.CutSuffix(configured, "-latest"); isAlias && stem != "" {
		if _, served := findServed(cat, stem); served || len(versionsOf(cat, stem)) > 0 {
			return configured, ""
		}
	}
	model = pickServed(provider, configured, cat)
	if configured != "" {
		note = fmt.Sprintf("%s no longer serves %s; using %s", provider, configured, model)
	}
	return model, note
}

func pickServed(provider, configured string, cat []CatalogModel) string {
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

// versionsOf returns the served models that are versions of id.
func versionsOf(cat []CatalogModel, id string) []CatalogModel {
	var out []CatalogModel
	for _, m := range cat {
		if rest, found := strings.CutPrefix(m.ID, id); found && versionSuffix.MatchString(rest) {
			out = append(out, m)
		}
	}
	return out
}

// compareVersions compares two version suffixes by their numbers, so v1.10
// is newer than v1.9.
func compareVersions(a, b string) int {
	na, nb := versionNumbers(a), versionNumbers(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if c := na[i].Cmp(nb[i]); c != 0 {
			return c
		}
	}
	return len(na) - len(nb)
}

var digitRuns = regexp.MustCompile(`\d+`)

func versionNumbers(s string) []*big.Int {
	var out []*big.Int
	for _, part := range digitRuns.FindAllString(s, -1) {
		n, _ := new(big.Int).SetString(part, 10)
		out = append(out, n)
	}
	return out
}
