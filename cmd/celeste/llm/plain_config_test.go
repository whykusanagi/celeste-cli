package llm

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

func TestConfigFromPlainClearsXAIExtras(t *testing.T) {
	cfg := &config.Config{
		APIKey:                "k",
		BaseURL:               "https://api.example.com/v1",
		Model:                 "m",
		GoogleCredentialsFile: "sa.json",
		Collections:           &config.CollectionsConfig{Enabled: true},
		XAIFeatures:           &config.XAIFeaturesConfig{},
	}
	got := ConfigFrom(cfg).Plain()
	if got.Collections != nil || got.XAIFeatures != nil {
		t.Fatalf("Plain kept an xAI field: %+v", got)
	}
	if got.APIKey != "k" || got.BaseURL != cfg.BaseURL || got.Model != "m" || got.GoogleCredentialsFile != "sa.json" {
		t.Fatalf("Plain dropped a connection field: %+v", got)
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
