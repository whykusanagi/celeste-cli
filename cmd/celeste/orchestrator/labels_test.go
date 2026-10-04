package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/orchestrator"
)

// V21: the primary-agent action carried its model in both Model and Text,
// and the TUI prefixes Model, so the feed read "[fugu] [fugu] primary
// agent". A goal no lane keyword matched read "unknown · 10% confidence",
// a made-up number.
func TestOrchestratorEventLabels(t *testing.T) {
	cfg := &config.Config{Model: "fugu"}
	var events []orchestrator.OrchestratorEvent
	o := orchestrator.New(cfg, orchestrator.WithRunnerFactory(func(string) orchestrator.AgentRunner {
		return &fakeRunner{response: "done"}
	}))
	o.OnEvent(func(e orchestrator.OrchestratorEvent) { events = append(events, e) })
	if _, err := o.Run(context.Background(), "printf hi"); err != nil {
		t.Fatal(err)
	}
	var sawPrimary, sawClassified bool
	for _, e := range events {
		if e.Kind == orchestrator.EventAction && e.Model == "fugu" && strings.Contains(e.Text, "primary agent") {
			sawPrimary = true
			if strings.Contains(e.Text, "fugu") {
				t.Errorf("primary-agent text repeats the model: %q", e.Text)
			}
		}
		if e.Kind == orchestrator.EventClassified {
			sawClassified = true
			if e.Lane != orchestrator.LaneUnknown {
				t.Fatalf("lane = %s, want unknown", e.Lane)
			}
			if strings.Contains(e.Text, "confidence") {
				t.Errorf("an unmatched goal reports a confidence: %q", e.Text)
			}
		}
	}
	if !sawPrimary || !sawClassified {
		t.Fatalf("missing events: primary=%v classified=%v", sawPrimary, sawClassified)
	}
}
