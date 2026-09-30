package main

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// needsAPIKey reports whether cfg needs a non-empty api_key before a run can
// start. It is false for Google ADC/service-account auth
// (google_use_adc, google_credentials_file) and for a provider whose
// registry entry needs no key — currently only local OpenAI-compatible
// servers detected from base_url (#151). agent_run.go's ADC-only check was
// the origin of this rule; runChatTUI, runSingleMessage and RunAgent now all
// call this one helper instead of duplicating it, so a keyless local
// endpoint (#151) no longer has to fail the same check ADC already passes.
func needsAPIKey(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	if cfg.GoogleUseADC || cfg.GoogleCredentialsFile != "" {
		return false
	}
	if caps, ok := providers.GetProvider(providers.DetectProvider(cfg.BaseURL)); ok && !caps.RequiresAPIKey {
		return false
	}
	return true
}
