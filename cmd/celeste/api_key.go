package main

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// needsAPIKey reports whether cfg needs a non-empty api_key before a run can
// start. It is false for Google ADC/service-account auth
// (google_use_adc, google_credentials_file) when base_url actually resolves
// to a Google backend, and for a provider whose registry entry needs no key
// on its own (RequiresAPIKey: false), detected generically from base_url —
// currently local OpenAI-compatible servers and Vertex, though Vertex is
// already covered by the ADC branch above before this one is ever reached
// (#151). agent_run.go's ADC-only check was the origin of this rule;
// runChatTUI, runSingleMessage and RunAgent now all call this one helper
// (and so does /agent's TUIClientAdapter, #144/#151 W6b) instead of
// duplicating it, so a keyless local endpoint (#151) no longer has to fail
// the same check ADC already passes.
//
// The base_url check (not just the two Google fields on their own) matters
// because --set-url repoints a profile's provider without clearing a
// previously-set google_use_adc/google_credentials_file (main.go never
// clears them): a stale ADC flag left over from an earlier Vertex/Gemini
// setup must not wave through a now-OpenAI-or-similar profile that has no
// api_key and genuinely needs one. llm.NewGoogleBackend only ever engages
// for a generativelanguage.googleapis.com or aiplatform.googleapis.com
// base_url (llm/interface.go), so this mirrors that same detection.
func needsAPIKey(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	if cfg.GoogleUseADC || cfg.GoogleCredentialsFile != "" {
		switch providers.DetectProvider(cfg.BaseURL) {
		case "gemini", "vertex":
			return false
		}
	}
	if caps, ok := providers.GetProvider(providers.DetectProvider(cfg.BaseURL)); ok && !caps.RequiresAPIKey {
		return false
	}
	return true
}
