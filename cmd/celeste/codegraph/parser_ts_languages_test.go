package codegraph

import (
	"testing"
)

func TestSupportedLanguage(t *testing.T) {
	if got := SupportedLanguage(".py"); got != "python" {
		t.Errorf("SupportedLanguage(.py) = %q, want python", got)
	}
	if got := SupportedLanguage(".rs"); got != "rust" {
		t.Errorf("SupportedLanguage(.rs) = %q, want rust", got)
	}
	if got := SupportedLanguage(".xyz"); got != "" {
		t.Errorf("SupportedLanguage(.xyz) = %q, want empty", got)
	}
}

func TestNodeTypeSet(t *testing.T) {
	set := nodeTypeSet([]string{"a", "b", "c"})
	if !set["a"] || !set["b"] || !set["c"] {
		t.Error("nodeTypeSet should contain all input values")
	}
	if set["d"] {
		t.Error("nodeTypeSet should not contain values not in input")
	}
}
