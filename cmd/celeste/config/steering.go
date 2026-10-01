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
