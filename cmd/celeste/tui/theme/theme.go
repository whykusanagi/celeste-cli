// Package theme exposes the canonical corrupted-theme color palette, embedded
// from colors.json. The JSON is synced from the corrupted-theme repo
// (src/data/colors.json) via `make sync-theme` so celeste-cli's corruption
// colors stay in lockstep with the theme instead of drifting from hardcoded
// hex literals (task 7aa133c9; the #49 color-drift fix is the motivation).
package theme

import (
	_ "embed"
	"encoding/json"
)

//go:embed colors.json
var colorsJSON []byte

// Colors is the parsed palette. Keys mirror corrupted-theme's colors.json.
type Colors struct {
	SchemaVersion string            `json:"schemaVersion"`
	Palette       map[string]string `json:"palette"`
	SemanticUse   map[string]string `json:"semanticUse"`
}

var palette Colors

func init() {
	if err := json.Unmarshal(colorsJSON, &palette); err != nil {
		// The JSON is embedded at build time and validated by tests; a parse
		// failure here means a corrupt sync, which we want to fail loudly.
		panic("theme: invalid embedded colors.json: " + err.Error())
	}
}

// Hex returns the hex string for a palette key (e.g. "cyan"); "" if absent.
func Hex(key string) string { return palette.Palette[key] }

// Semantic returns the hex for a semantic role (e.g. "corrupting"), resolved
// through the palette. "" if the role or its palette key is missing.
func Semantic(role string) string {
	return palette.Palette[palette.SemanticUse[role]]
}
