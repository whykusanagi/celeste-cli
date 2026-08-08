package providers

import "testing"

func TestOrchestratesServerSide(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		modelID  string
		want     bool
	}{
		{"fugu standard", "sakana", "fugu", true},
		{"fugu ultra", "sakana", "fugu-ultra", true},
		{"fugu dated pin", "sakana", "fugu-ultra-20260615", true},
		{"fugu v1.0", "sakana", "fugu-ultra-v1.0", true},
		{"fugu v1.1", "sakana", "fugu-ultra-v1.1", true},
		// Not in our static list and not in this account's /v1/models, but
		// documented by Sakana. Must not fall back to local planning.
		{"fugu cyber", "sakana", "fugu-cyber", true},
		// The reason this is a rule and not a table lookup: Sakana ships
		// version bumps faster than the static list is updated.
		{"unreleased fugu bump", "sakana", "fugu-ultra-v9.9", true},
		// Sakana serves an OpenAI-compatible API and could host a plain
		// model. A plain model on that endpoint still needs local planning.
		{"plain model on sakana", "sakana", "gpt-4.1-nano", false},
		{"openai", "openai", "gpt-4.1-nano", false},
		// Guard against a bare prefix match on the wrong provider.
		{"fugu-named model elsewhere", "openai", "fugu", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OrchestratesServerSide(tt.provider, tt.modelID); got != tt.want {
				t.Errorf("OrchestratesServerSide(%q, %q) = %v, want %v",
					tt.provider, tt.modelID, got, tt.want)
			}
		})
	}
}

func TestStaticSakanaModelsAreFlagged(t *testing.T) {
	s := NewModelService("", "https://api.sakana.ai/v1", "sakana")
	models := s.getStaticModels()
	if len(models) == 0 {
		t.Fatal("no static sakana models")
	}
	seen := map[string]bool{}
	for _, m := range models {
		seen[m.ID] = true
		if !m.OrchestratesServerSide {
			t.Errorf("model %q: OrchestratesServerSide = false, want true", m.ID)
		}
	}
	// Live /v1/models on 2026-08-07 returned these five.
	for _, id := range []string{"fugu", "fugu-ultra", "fugu-ultra-20260615", "fugu-ultra-v1.0", "fugu-ultra-v1.1"} {
		if !seen[id] {
			t.Errorf("static model list is missing %q", id)
		}
	}
}
