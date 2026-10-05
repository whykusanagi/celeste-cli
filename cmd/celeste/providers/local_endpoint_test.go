package providers

import "testing"

// IsLocalEndpoint: a server on this machine or the local network, which
// gets the slow-model timeout default. Hosted providers never do.
func TestIsLocalEndpoint(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:11434/v1":                    true,
		"http://localhost:1234/v1":                     true,
		"http://[::1]:8080/v1":                         true,
		"http://0.0.0.0:8080/v1":                       true,
		"http://192.168.1.20:11434/v1":                 true,
		"http://10.0.0.5:8000/v1":                      true,
		"http://172.16.3.4:8000/v1":                    true,
		"http://[fd00::2]:8000/v1":                     true,
		"http://gpubox.local:11434/v1":                 true,
		"http://gpubox:11434/v1":                       true,
		"http://host.docker.internal:1234":             true,
		"https://api.sakana.ai/v1":                     false,
		"https://api.openai.com/v1":                    false,
		"https://api.x.ai/v1":                          false,
		"https://openrouter.ai/api/v1":                 false,
		"https://my-proxy.example.com/v1":              false,
		"http://8.8.8.8/v1":                            false,
		"":                                             false,
		"https://api.anthropic.com":                    false,
		"https://your-agent.ondigitalocean.app/api/v1": false,
	} {
		if got := IsLocalEndpoint(url); got != want {
			t.Errorf("IsLocalEndpoint(%q) = %v, want %v", url, got, want)
		}
	}
}

// IsLocalHost decides on the parsed host only, so a hosted URL that merely
// contains "localhost" is not local (the window probe must never reach it).
func TestIsLocalHost(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:11434/v1":         true,
		"http://localhost:1234/v1":          true,
		"http://[::1]:8080/v1":              true,
		"http://192.168.1.20:11434/v1":      true,
		"http://gpu-box.local:8080/v1":      true,
		"http://studio:1234/v1":             true,
		"https://localhost.evil.example/v1": false,
		"https://example.com/localhost/v1":  false,
		"https://api.openai.com/v1":         false,
		"https://8.8.8.8/v1":                false,
		"":                                  false,
	} {
		if got := IsLocalHost(url); got != want {
			t.Errorf("IsLocalHost(%q) = %v, want %v", url, got, want)
		}
	}
}

// One rule decides "is this server local" for the timeouts, the window
// probe, the 8,192 fallback and the tool gate (#377): the parsed host. A
// LAN, bare-hostname or .local server is local everywhere; a hosted URL
// whose path merely says "localhost" is local nowhere.
func TestLocalRuleIsOneRule(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:11434/v1":              true,
		"http://[::1]:8080/v1":                   true,
		"http://localhost:1234/v1":               true,
		"localhost:1234/v1":                      true,
		"127.0.0.1:8080/v1":                      true,
		"http://192.168.1.20:8080/v1":            true,
		"http://10.0.0.5:8000/v1":                true,
		"http://172.16.3.4:8000/v1":              true,
		"http://172.31.255.1:8000/v1":            true,
		"http://gpu-box:8080/v1":                 true,
		"http://mac.local:1234/v1":               true,
		"http://172.32.0.1:8000/v1":              false,
		"https://proxy.example.com/localhost/v1": false,
		"https://proxy.example.com/127.0.0.1/v1": false,
		"https://localhost.evil.example/v1":      false,
		"https://api.openai.com/v1":              false,
		"":                                       false,
	} {
		if got := IsLocalHost(url); got != want {
			t.Errorf("IsLocalHost(%q) = %v, want %v", url, got, want)
		}
		if got := IsLocalEndpoint(url); got != want {
			t.Errorf("IsLocalEndpoint(%q) = %v, want %v", url, got, want)
		}
		if got := DetectProvider(url) == "local"; got != want {
			t.Errorf("DetectProvider(%q) = %q, local want %v", url, DetectProvider(url), want)
		}
		// The tool gate follows: a local server gets tools whatever the
		// model is called (the seed model fugu included).
		if want && !ToolsEnabledForModel(DetectProvider(url), "fugu") {
			t.Errorf("ToolsEnabledForModel(DetectProvider(%q), fugu) = false, want true", url)
		}
	}
}

// A LAN server whose path names a hosted provider is still the LAN server:
// the local host check runs before the remaining substring matches.
func TestDetectProviderLocalHostBeatsPathSubstring(t *testing.T) {
	for _, u := range []string{
		"http://192.168.1.20:8080/openai.com/v1",
		"http://gpu-box:8080/openrouter.ai/v1",
	} {
		if got := DetectProvider(u); got != "local" {
			t.Errorf("DetectProvider(%q) = %q, want local", u, got)
		}
	}
}
