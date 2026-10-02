package config

import "testing"

// Steering modes default when unset or unknown (2.0 W3): an unknown value
// is never read as "on".
func TestOracleModeDefaults(t *testing.T) {
	for in, want := range map[string]string{"": "heuristic", "jev": "jev", "llm": "llm", "JEV": "heuristic", "on": "heuristic"} {
		if got := (&Config{Oracle: in}).OracleMode(); got != want {
			t.Errorf("oracle %q = %q, want %q", in, got, want)
		}
	}
}

func TestStreamRulesModeDefaultsToShadow(t *testing.T) {
	for in, want := range map[string]string{"": "shadow", "on": "on", "off": "off", "yes": "shadow"} {
		if got := (&Config{StreamRules: in}).StreamRulesMode(); got != want {
			t.Errorf("stream_rules %q = %q, want %q", in, got, want)
		}
	}
}
