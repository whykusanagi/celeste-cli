package providers

import "testing"

func TestParseOpenRouterToolSupport(t *testing.T) {
	body := []byte(`{"data":[
		{"id":"openai/gpt-4o","supported_parameters":["tools","temperature","max_tokens"]},
		{"id":"meta-llama/llama-3-8b-instruct","supported_parameters":["temperature","top_p"]},
		{"id":"anthropic/claude-3.5-sonnet","supported_parameters":["tools"]},
		{"id":"some/no-params"}
	]}`)
	m := parseOpenRouterToolSupport(body)

	cases := map[string]bool{
		"openai/gpt-4o":                  true,
		"meta-llama/llama-3-8b-instruct": false, // no "tools" -> agent mode would flail
		"anthropic/claude-3.5-sonnet":    true,
		"some/no-params":                 false,
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

func TestParseOpenRouterToolSupport_BadJSON(t *testing.T) {
	if m := parseOpenRouterToolSupport([]byte("not json")); len(m) != 0 {
		t.Fatalf("bad JSON should yield empty map, got %d entries", len(m))
	}
}

func TestLookupToolSupport(t *testing.T) {
	catalog := map[string]bool{
		"openai/gpt-4o":           true,
		"meta-llama/llama-3:free": false,
		"x/base":                  true,
	}
	// exact hit
	if s, known := lookupToolSupport(catalog, "openai/gpt-4o"); !known || !s {
		t.Errorf("exact: got (%v,%v), want (true,true)", s, known)
	}
	// :variant suffix falls back to the base id
	if s, known := lookupToolSupport(catalog, "x/base:nitro"); !known || !s {
		t.Errorf("variant: got (%v,%v), want (true,true)", s, known)
	}
	// unknown model -> known=false so caller can fall back to heuristic
	if _, known := lookupToolSupport(catalog, "who/dis"); known {
		t.Errorf("unknown model should be known=false")
	}
	// empty catalog -> known=false
	if _, known := lookupToolSupport(nil, "openai/gpt-4o"); known {
		t.Errorf("empty catalog should be known=false")
	}
}
