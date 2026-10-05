package llm

import (
	"reflect"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

func TestConfigFromCarriesEveryField(t *testing.T) {
	cfg := &config.Config{
		APIKey:                "k",
		BaseURL:               "https://api.example.com/v1",
		Model:                 "m",
		Timeout:               90,
		SimulateTyping:        true,
		TypingSpeed:           75,
		GoogleCredentialsFile: "sa.json",
		GoogleUseADC:          true,
		Collections:           &config.CollectionsConfig{Enabled: true},
		XAIFeatures:           &config.XAIFeaturesConfig{},
	}
	got := ConfigFrom(cfg)
	want := &Config{
		APIKey: "k", BaseURL: "https://api.example.com/v1", Model: "m",
		Timeout:               90 * time.Second,
		FirstByteTimeout:      90 * time.Second,
		GoogleCredentialsFile: "sa.json", GoogleUseADC: true,
		Collections: cfg.Collections, XAIFeatures: cfg.XAIFeatures,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfigFrom = %+v, want %+v", got, want)
	}
}
