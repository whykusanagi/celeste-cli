package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// #151: local endpoints and Google ADC/service-account auth need no
// api_key. agent_run.go already special-cased ADC; this pins the shared
// helper runChatTUI, runSingleMessage and RunAgent all call.
func TestNeedsAPIKey(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"remote provider with no key needs one", &config.Config{BaseURL: "https://api.openai.com/v1"}, true},
		{"vertex ADC needs no key", &config.Config{BaseURL: "https://us-central1-aiplatform.googleapis.com", GoogleUseADC: true}, false},
		{"google service account file needs no key", &config.Config{BaseURL: "https://us-central1-aiplatform.googleapis.com", GoogleCredentialsFile: "/tmp/sa.json"}, false},
		{"localhost endpoint needs no key", &config.Config{BaseURL: "http://127.0.0.1:8080/v1"}, false},
		{"named localhost endpoint needs no key", &config.Config{BaseURL: "http://localhost:11434/v1"}, false},
		{"remote provider still needs a key even with ADC fields unset", &config.Config{BaseURL: "https://api.venice.ai/api/v1"}, true},
		{"nil config defaults to needing a key", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, needsAPIKey(tt.cfg))
		})
	}
}
