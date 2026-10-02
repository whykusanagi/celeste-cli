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
func TestWatchdogModeDefaultsToOff(t *testing.T) {
	for in, want := range map[string]string{"": "off", "on": "on", "shadow": "shadow", "true": "off"} {
		if got := (&Config{Watchdog: in}).WatchdogMode(); got != want {
			t.Errorf("watchdog %q = %q, want %q", in, got, want)
		}
	}
}

func TestCompletionGateModeDefaultsToShadow(t *testing.T) {
	for in, want := range map[string]string{"": "shadow", "on": "on", "off": "shadow"} {
		if got := (&Config{CompletionGate: in}).CompletionGateMode(); got != want {
			t.Errorf("completion_gate %q = %q, want %q", in, got, want)
		}
	}
}

func TestJevPruneModeDefaultsToOff(t *testing.T) {
	for in, want := range map[string]string{"": "off", "shadow": "shadow", "on": "on", "yes": "off"} {
		if got := (&Config{JevPrune: in}).JevPruneMode(); got != want {
			t.Errorf("jev_prune %q = %q, want %q", in, got, want)
		}
	}
}
