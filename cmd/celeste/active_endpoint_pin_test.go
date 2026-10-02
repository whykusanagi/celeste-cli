package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
)

// CELESTE_PIN_MODEL is read in one place (config.ModelPinned), with or
// without a base config; pin_model in the config pins too.
func TestActiveEndpointPinned(t *testing.T) {
	client := llm.NewClientWithBackend(llm.ConfigFrom(&config.Config{BaseURL: "https://api.openai.com/v1", Model: "m"}), nil, nil)

	t.Setenv("CELESTE_PIN_MODEL", "1")
	if !(&TUIClientAdapter{client: client}).ActiveEndpoint().Pinned {
		t.Error("CELESTE_PIN_MODEL=1 without a base config: not pinned")
	}
	if !(&TUIClientAdapter{client: client, baseConfig: &config.Config{}}).ActiveEndpoint().Pinned {
		t.Error("CELESTE_PIN_MODEL=1 with a base config: not pinned")
	}

	t.Setenv("CELESTE_PIN_MODEL", "")
	if (&TUIClientAdapter{client: client}).ActiveEndpoint().Pinned {
		t.Error("pinned with nothing set")
	}
	if !(&TUIClientAdapter{client: client, baseConfig: &config.Config{PinModel: true}}).ActiveEndpoint().Pinned {
		t.Error("pin_model: not pinned")
	}
}
