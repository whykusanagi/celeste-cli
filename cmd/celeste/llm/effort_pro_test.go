package llm

import "testing"

// gpt-5-pro takes only the high reasoning effort; any level celeste
// would send becomes high, none stays none.
func TestOpenAIEffortGPT5Pro(t *testing.T) {
	for _, model := range []string{"gpt-5-pro", "gpt-5-pro-2025-10-06", "GPT-5-PRO"} {
		for _, level := range []string{"low", "medium", "high", "max"} {
			if got := openAIEffort(model, ThinkingConfig{Enabled: true, Level: level}); got != "high" {
				t.Errorf("openAIEffort(%s, %s) = %q, want high", model, level, got)
			}
		}
		if got := openAIEffort(model, ThinkingConfig{}); got != "" {
			t.Errorf("openAIEffort(%s, off) = %q, want none", model, got)
		}
	}
	if got := openAIEffort("gpt-5", ThinkingConfig{Enabled: true, Level: "low"}); got != "low" {
		t.Errorf("gpt-5 low = %q", got)
	}
}
