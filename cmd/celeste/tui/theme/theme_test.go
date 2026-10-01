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

// Unknown keys return empty, not a panic.
func TestUnknownKey(t *testing.T) {
	if got := Hex("nope"); got != "" {
		t.Errorf("Hex(unknown) = %q, want empty", got)
	}
}
