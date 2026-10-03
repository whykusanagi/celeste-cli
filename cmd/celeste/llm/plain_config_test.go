package llm

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

func TestPlainConfigFromClearsXAIExtras(t *testing.T) {
	cfg := &config.Config{
		APIKey:                "k",
		BaseURL:               "https://api.example.com/v1",
		Model:                 "m",
		GoogleCredentialsFile: "sa.json",
		Collections:           &config.CollectionsConfig{Enabled: true},
		XAIFeatures:           &config.XAIFeaturesConfig{},
	}
	got := PlainConfigFrom(cfg)
	if got.Collections != nil || got.XAIFeatures != nil {
		t.Fatalf("PlainConfigFrom kept an xAI field: %+v", got)
	}
	if got.APIKey != "k" || got.BaseURL != cfg.BaseURL || got.Model != "m" || got.GoogleCredentialsFile != "sa.json" {
		t.Fatalf("PlainConfigFrom dropped a connection field: %+v", got)
	}
}

func TestPlainDoesNotModifyReceiver(t *testing.T) {
	c := &Config{Collections: &config.CollectionsConfig{}}
	p := c.Plain()
	if p == c {
		t.Fatal("Plain returned its receiver")
	}
	if c.Collections == nil {
		t.Fatalf("Plain modified its receiver: %+v", c)
	}
}
