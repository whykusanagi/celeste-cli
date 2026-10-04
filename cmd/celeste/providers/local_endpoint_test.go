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
