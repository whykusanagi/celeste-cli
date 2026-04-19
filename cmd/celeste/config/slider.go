// Package config — personality slider persistence.
//
// Sliders modulate Celeste's voice inside whatever register is active.
// They do NOT select registers, override invariants, or touch canonical
// collection files. They compose ON TOP of the persona baseline at
// position 6 in the assembly order (after voice_and_tone, before
// active register body).
//
// Persistence: ~/.celeste/slider.yaml (or slider.json — we use JSON
// to match the existing config pattern). Loaded at startup, written on
// every slider change via the /persona TUI panel.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SliderConfig holds the user's personality slider state.
// Each slider is an int 0-10. Anchor points at 0/3/7/10 snap to
// authored prompt snippets; intermediate values snap to nearest anchor.
type SliderConfig struct {
	// Flirt controls how forward / teasing she is.
	// 0=professional, 3=playful, 7=flirty, 10=aggressive-flirt
	Flirt int `json:"flirt"`

	// Warmth controls affection level.
	// 0=cold/distant, 3=polite, 7=warm, 10=openly-affectionate
	Warmth int `json:"warmth"`

	// Register controls speech style.
	// 0=clipped-operator, 3=standard, 7=theatrical, 10=uwu/baby-talk
	Register int `json:"register"`

	// Lewdness controls content eligibility. Only has effect when
	// R18Enabled is true — without R18, lewdness > 0 is a no-op.
	// 0=SFW, 3=suggestive, 7=explicit-tease, 10=R18
	Lewdness int `json:"lewdness"`

	// R18Enabled is the independent toggle. Provider-agnostic — it
	// controls what Celeste ATTEMPTS, not what the backend allows.
	// Default false. Orthogonal to every slider.
	R18Enabled bool `json:"r18_enabled"`

	// Presets stores user-saved named slider combos.
	Presets map[string]SliderPreset `json:"presets,omitempty"`
}

// SliderPreset is a named snapshot of slider values.
type SliderPreset struct {
	Flirt      int  `json:"flirt"`
	Warmth     int  `json:"warmth"`
	Register   int  `json:"register"`
	Lewdness   int  `json:"lewdness"`
	R18Enabled bool `json:"r18_enabled"`
}

// DefaultSliderConfig returns the factory-default slider state.
// Moderate across the board, R18 off.
func DefaultSliderConfig() *SliderConfig {
	return &SliderConfig{
		Flirt:      3,
		Warmth:     5,
		Register:   3,
		Lewdness:   0,
		R18Enabled: false,
		Presets:    map[string]SliderPreset{},
	}
}

// SliderPath returns the path to the slider config file.
func SliderPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "slider.json")
}

// LoadSliders loads the slider config from disk. Returns defaults if
// the file doesn't exist or is malformed.
func LoadSliders() *SliderConfig {
	data, err := os.ReadFile(SliderPath())
	if err != nil {
		return DefaultSliderConfig()
	}
	var cfg SliderConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultSliderConfig()
	}
	cfg.clamp()
	if cfg.Presets == nil {
		cfg.Presets = map[string]SliderPreset{}
	}
	return &cfg
}

// Save writes the slider config to disk.
func (s *SliderConfig) Save() error {
	s.clamp()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal slider config: %w", err)
	}
	dir := filepath.Dir(SliderPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(SliderPath(), data, 0644)
}

// SavePreset snapshots the current slider values under a name.
func (s *SliderConfig) SavePreset(name string) {
	if s.Presets == nil {
		s.Presets = map[string]SliderPreset{}
	}
	s.Presets[name] = SliderPreset{
		Flirt:      s.Flirt,
		Warmth:     s.Warmth,
		Register:   s.Register,
		Lewdness:   s.Lewdness,
		R18Enabled: s.R18Enabled,
	}
}

// LoadPreset restores slider values from a named preset.
func (s *SliderConfig) LoadPreset(name string) bool {
	p, ok := s.Presets[name]
	if !ok {
		return false
	}
	s.Flirt = p.Flirt
	s.Warmth = p.Warmth
	s.Register = p.Register
	s.Lewdness = p.Lewdness
	s.R18Enabled = p.R18Enabled
	s.clamp()
	return true
}

// Reset restores factory defaults (preserves presets).
func (s *SliderConfig) Reset() {
	presets := s.Presets
	*s = *DefaultSliderConfig()
	s.Presets = presets
}

// clamp ensures all slider values are within [0, 10].
func (s *SliderConfig) clamp() {
	s.Flirt = clampInt(s.Flirt, 0, 10)
	s.Warmth = clampInt(s.Warmth, 0, 10)
	s.Register = clampInt(s.Register, 0, 10)
	s.Lewdness = clampInt(s.Lewdness, 0, 10)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SnapToAnchor returns the nearest anchor index (0-3) for a slider
// value 0-10. Anchors are at 0, 3, 7, 10.
func SnapToAnchor(value int) int {
	switch {
	case value <= 1:
		return 0
	case value <= 5:
		return 1
	case value <= 8:
		return 2
	default:
		return 3
	}
}
