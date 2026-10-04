package main

import (
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// #345: the chat's summaries are bounded by the summary client's request
// cap, from the profile's timeout as a chat turn's is.
func TestAdapterSummaryTimeoutFollowsTheProfile(t *testing.T) {
	for _, tc := range []struct {
		cfg  *config.Config
		want time.Duration
	}{
		{nil, 30 * time.Minute},
		{&config.Config{BaseURL: "http://localhost:11434/v1"}, 30 * time.Minute},
		{&config.Config{Timeout: 1200}, 60 * time.Minute},
	} {
		if got := (&TUIClientAdapter{baseConfig: tc.cfg}).SummaryTimeout(); got != tc.want {
			t.Errorf("SummaryTimeout(%+v) = %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

// tui cannot import llm, so its fallback summary bound is a copy of the
// default request cap. This keeps the copy in step with llm.
func TestTUIDefaultSummaryTimeoutMatchesTheDefaultRequestCap(t *testing.T) {
	if want := (*llm.Config)(nil).RequestCap(); tui.DefaultSummaryTimeout != want {
		t.Errorf("tui.DefaultSummaryTimeout = %v, want llm's default request cap %v", tui.DefaultSummaryTimeout, want)
	}
}
