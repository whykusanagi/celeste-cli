package providers

import (
	"fmt"
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
		return preferTools(defaults)[0].ID
	}

	if configured != "" {
		var versions []CatalogModel
		for _, m := range cat {
			rest, found := strings.CutPrefix(m.ID, configured)
			if found && rest != "" && strings.ContainsRune("-.:", rune(rest[0])) {
				versions = append(versions, m)
			}
		}
		if len(versions) > 0 {
			best := ""
			for _, m := range preferTools(versions) {
				if m.ID > best {
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

	return preferTools(cat)[0].ID
}

// preferTools returns the tool-capable models if there are any, else all of
// them, keeping their order.
func preferTools(models []CatalogModel) []CatalogModel {
	var tools []CatalogModel
	for _, m := range models {
		if m.Tools != nil && *m.Tools {
			tools = append(tools, m)
		}
	}
	if len(tools) > 0 {
		return tools
	}
	return models
}
