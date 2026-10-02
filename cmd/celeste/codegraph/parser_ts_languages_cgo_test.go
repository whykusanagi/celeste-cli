//go:build cgo

// langSpecs exists only in cgo builds (tree-sitter); the extension lookup
// tests in parser_ts_languages_test.go run in both.
package codegraph

import "testing"

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
