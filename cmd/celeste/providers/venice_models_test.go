package providers

import "testing"

func TestParseVeniceToolSupport(t *testing.T) {
	// Shape mirrors api.venice.ai/api/v1/models. Note: an *-uncensored model can
	// support tools, and an e2ee-*-uncensored one may not — the old name heuristic
	// got both wrong, which is the whole point of using the catalog.
	body := []byte(`{"data":[
		{"id":"venice-uncensored-1-2","model_spec":{"capabilities":{"supportsFunctionCalling":true}}},
		{"id":"e2ee-venice-uncensored-24b-p","model_spec":{"capabilities":{"supportsFunctionCalling":false}}},
		{"id":"zai-org-glm-5","model_spec":{"capabilities":{"supportsFunctionCalling":true}}},
		{"id":"some-image-model","type":"image"}
	]}`)
	m := parseVeniceToolSupport(body)

	cases := map[string]bool{
		"venice-uncensored-1-2":        true,  // heuristic would WRONGLY block this
		"e2ee-venice-uncensored-24b-p": false,
		"zai-org-glm-5":                true,
		"some-image-model":             false, // no capabilities -> false
	}
	for id, want := range cases {
		got, ok := m[id]
		if !ok {
			t.Errorf("model %q missing from parsed catalog", id)
			continue
		}
		if got != want {
			t.Errorf("tool support for %q = %v, want %v", id, got, want)
		}
	}
}

func TestParseVeniceToolSupport_BadJSON(t *testing.T) {
	if m := parseVeniceToolSupport([]byte("nope")); len(m) != 0 {
		t.Fatalf("bad JSON should yield empty map, got %d", len(m))
	}
}
