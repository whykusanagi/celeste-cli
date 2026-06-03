package theme

import "testing"

// The embedded palette must carry the canonical corrupted-theme colors
// (the #49 fix: cyan #00ffff / red #ff0000).
func TestPaletteCanonicalColors(t *testing.T) {
	cases := map[string]string{
		"cyan":     "#00ffff",
		"red":      "#ff0000",
		"magenta2": "#d94f90",
		"purple":   "#8b5cf6",
	}
	for key, want := range cases {
		if got := Hex(key); got != want {
			t.Errorf("Hex(%q) = %q, want %q", key, got, want)
		}
	}
}

// Semantic roles resolve through the palette.
func TestSemanticResolution(t *testing.T) {
	if got := Semantic("decoded"); got != "#00ffff" {
		t.Errorf("Semantic(decoded) = %q, want #00ffff", got)
	}
	if got := Semantic("critical"); got != "#ff0000" {
		t.Errorf("Semantic(critical) = %q, want #ff0000", got)
	}
}

// Unknown keys return empty, not a panic.
func TestUnknownKey(t *testing.T) {
	if got := Hex("nope"); got != "" {
		t.Errorf("Hex(unknown) = %q, want empty", got)
	}
}
