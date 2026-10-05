package config

import (
	"testing"
	"time"
)

// L1: a local model needs minutes for a cold first turn, so a local endpoint
// left at the generic default gets the local default. Hosted providers keep
// the generic one, and a value the user chose is kept everywhere.
func TestGetTimeoutDefaults(t *testing.T) {
	local, hosted := "http://127.0.0.1:11434/v1", "https://api.sakana.ai/v1"
	for _, tc := range []struct {
		name    string
		baseURL string
		timeout int
		want    time.Duration
	}{
		{"hosted unset", hosted, 0, 60 * time.Second},
		{"hosted generic default", hosted, 60, 60 * time.Second},
		{"hosted chosen", hosted, 300, 300 * time.Second},
		{"local unset", local, 0, LocalTimeoutSeconds * time.Second},
		{"local generic default", local, DefaultTimeoutSeconds, LocalTimeoutSeconds * time.Second},
		{"local chosen higher", local, 900, 900 * time.Second},
		{"local chosen lower", local, 30, 30 * time.Second},
		{"lan unset", "http://192.168.1.20:11434/v1", 0, LocalTimeoutSeconds * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{BaseURL: tc.baseURL, Timeout: tc.timeout}
			if got := c.GetTimeout(); got != tc.want {
				t.Errorf("GetTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
	if DefaultConfig().Timeout != DefaultTimeoutSeconds {
		t.Errorf("DefaultConfig().Timeout = %d, want DefaultTimeoutSeconds", DefaultConfig().Timeout)
	}
	if LocalTimeoutSeconds < 600 {
		t.Errorf("LocalTimeoutSeconds = %d: a cold 16K prefill on a 14B ran past 300 s", LocalTimeoutSeconds)
	}
}

// #359: a local server gets a first-byte budget far beyond its stall
// timeout (a cold prefill on a loaded machine took ~563 s); hosted
// providers keep their timeout for the first byte too.
func TestGetFirstByteTimeout(t *testing.T) {
	local, hosted := "http://127.0.0.1:11434/v1", "https://api.sakana.ai/v1"
	for _, tc := range []struct {
		name    string
		baseURL string
		timeout int
		want    time.Duration
	}{
		{"hosted unset", hosted, 0, 60 * time.Second},
		{"hosted chosen", hosted, 300, 300 * time.Second},
		{"local unset", local, 0, LocalFirstByteSeconds * time.Second},
		{"local chosen lower", local, 30, LocalFirstByteSeconds * time.Second},
		{"local chosen above the budget", local, 3600, 3600 * time.Second},
		{"lan host", "http://gpu-box:8080/v1", 0, LocalFirstByteSeconds * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{BaseURL: tc.baseURL, Timeout: tc.timeout}
			if got := c.GetFirstByteTimeout(); got != tc.want {
				t.Errorf("GetFirstByteTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
	if LocalFirstByteSeconds < 1800 {
		t.Errorf("LocalFirstByteSeconds = %d: a loaded machine took ~563 s to the first byte and 11.5 min after plan mode", LocalFirstByteSeconds)
	}
}
