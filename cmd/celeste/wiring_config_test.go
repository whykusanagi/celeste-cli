package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestChatPathsCarryGoogleCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeNamed(t, home, "other", `{"api_key":"k2","base_url":"http://127.0.0.1:2","model":"m2","google_credentials_file":"other-sa.json","google_use_adc":true}`)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10,
		GoogleCredentialsFile: "sa.json", GoogleUseADC: true}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	check := func(stage, file string) {
		t.Helper()
		got := deps.adapter.client.GetConfig()
		if got.GoogleCredentialsFile != file || !got.GoogleUseADC {
			t.Errorf("%s: GoogleCredentialsFile=%q GoogleUseADC=%v", stage, got.GoogleCredentialsFile, got.GoogleUseADC)
		}
	}
	check("startup", "sa.json")
	if err := deps.adapter.ChangeModel("another-model"); err != nil {
		t.Fatal(err)
	}
	check("model change", "sa.json")
	if err := deps.adapter.SwitchEndpoint("other"); err != nil {
		t.Fatal(err)
	}
	check("endpoint switch", "other-sa.json")
}

// Every llm.Config is built by llm.ConfigFrom; a hand-built literal drops
// whatever field its author forgot (google_credentials_file, once).
func TestNoHandBuiltLLMConfigLiterals(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(path), "llm/") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := parser.ParseFile(fset, path, src, 0); err != nil {
			return err
		}
		if strings.Contains(string(src), "llm.Config{") && !strings.Contains(path, "fakeprovider") {
			t.Errorf("%s builds an llm.Config literal; use llm.ConfigFrom", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSummarizerConfigKeepsItsSystemPromptAndSkipsXAIFeatures(t *testing.T) {
	cfg := &config.Config{APIKey: "k", BaseURL: "https://generativelanguage.googleapis.com/v1",
		SkipPersonaPrompt: true, GoogleCredentialsFile: "sa.json", Collections: &config.CollectionsConfig{Enabled: true}}
	got := summarizerConfig(cfg)
	if got.SkipPersonaPrompt {
		t.Error("the Google backend would drop the summary prompt")
	}
	if got.Collections != nil || got.XAIFeatures != nil || got.GoogleCredentialsFile != "sa.json" {
		t.Errorf("summarizer config: %+v", got)
	}
}
