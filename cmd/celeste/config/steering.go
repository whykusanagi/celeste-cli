package config

// Steering modes (2.0 W3, #175). Every mechanism is off or in shadow mode
// by default and fails open; an unknown value is the default.
const (
	ModeOff    = "off"
	ModeShadow = "shadow" // ask and log, never act
	ModeOn     = "on"
)

func mode(v, def string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

// OracleMode is oracle: "heuristic" (default), "llm" or "jev".
func (c *Config) OracleMode() string {
	return mode(c.Oracle, "heuristic", "heuristic", "llm", "jev")
}

// StreamRulesMode is stream_rules: "shadow" (default) matches and logs,
// "on" acts, "off" skips matching.
func (c *Config) StreamRulesMode() string {
	return mode(c.StreamRules, ModeShadow, ModeOff, ModeShadow, ModeOn)
}

// WatchdogMode is watchdog: "off" (default), "shadow" or "on".
func (c *Config) WatchdogMode() string {
	return mode(c.Watchdog, ModeOff, ModeOff, ModeShadow, ModeOn)
}

// CompletionGateMode is completion_gate: "shadow" (default) keeps the
// substring TASK_COMPLETE check and logs where the gate disagrees, "on"
// lets the gate decide.
func (c *Config) CompletionGateMode() string {
	return mode(c.CompletionGate, ModeShadow, ModeShadow, ModeOn)
}

// JevPruneMode is jev_prune: "off" (default), "shadow" or "on".
func (c *Config) JevPruneMode() string { return mode(c.JevPrune, ModeOff, ModeShadow, ModeOn) }
