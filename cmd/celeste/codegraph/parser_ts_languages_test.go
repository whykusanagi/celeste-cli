package codegraph

import (
	"testing"
)

func TestExtToLangCoversAllSpecs(t *testing.T) {
	// Every language in langSpecs should be reachable via at least one extension
	reachable := make(map[string]bool)
	for _, lang := range extToLang {
		reachable[lang] = true
	}
	for lang := range langSpecs {
		if !reachable[lang] {
			t.Errorf("language %q in langSpecs has no file extension mapping in extToLang", lang)
		}
	}
}

func TestLookupLangSpec(t *testing.T) {
	tests := []struct {
		ext  string
		lang string
		nil_ bool
	}{
		{".py", "python", false},
		{".rs", "rust", false},
		{".ts", "typescript", false},
		{".go", "go", false},
		{".java", "java", false},
		{".c", "c", false},
		{".cpp", "cpp", false},
		{".unknown", "", true},
		{".md", "", true},
	}
	for _, tt := range tests {
		spec := LookupLangSpec(tt.ext)
		if tt.nil_ {
			if spec != nil {
				t.Errorf("LookupLangSpec(%q) should return nil for unsupported extension", tt.ext)
			}
			continue
		}
		if spec == nil {
			t.Errorf("LookupLangSpec(%q) returned nil, want spec for %q", tt.ext, tt.lang)
			continue
		}
		if len(spec.FunctionTypes) == 0 {
			t.Errorf("LookupLangSpec(%q): FunctionTypes is empty", tt.ext)
		}
	}
}

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

func TestLangSpecCompleteness(t *testing.T) {
	// Core languages must have all four type mappings populated
	coreLangs := []string{"python", "rust", "go", "java", "typescript", "javascript"}
	for _, lang := range coreLangs {
		spec, ok := langSpecs[lang]
		if !ok {
			t.Errorf("core language %q missing from langSpecs", lang)
			continue
		}
		if len(spec.ClassTypes) == 0 {
			t.Errorf("%s: ClassTypes is empty", lang)
		}
		if len(spec.FunctionTypes) == 0 {
			t.Errorf("%s: FunctionTypes is empty", lang)
		}
		if len(spec.ImportTypes) == 0 {
			t.Errorf("%s: ImportTypes is empty", lang)
		}
		if len(spec.CallTypes) == 0 {
			t.Errorf("%s: CallTypes is empty", lang)
		}
		if spec.NameField == "" {
			t.Errorf("%s: NameField is empty", lang)
		}
	}
}
