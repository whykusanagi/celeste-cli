package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// endingBackend is windowBackend until request failAt, which ends the run:
// a provider error, or (cancel set) an interrupt. It keeps the history that
// last request was sent.
type endingBackend struct {
	*windowBackend
	failAt int
	cancel context.CancelFunc
	sent   []tui.ChatMessage
}

func (b *endingBackend) SendMessageStreamEvents(ctx context.Context, msgs []tui.ChatMessage, defs []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	n := b.requests
	if n >= b.failAt {
		b.requests++
		b.sent = append([]tui.ChatMessage(nil), msgs...)
		b.mu.Unlock()
		if b.cancel != nil {
			b.cancel()
			return ctx.Err()
		}
		return errors.New("upstream exploded")
	}
	b.mu.Unlock()
	return b.windowBackend.SendMessageStreamEvents(ctx, msgs, defs, cb)
}

// A prune just before a request that ends the run (an error or an
// interrupt) stays in the run's history, so the checkpoint and a resume see
// the history the model was last sent (2.0 F2e; the chat lost such a prune
// before #230). runState takes Run's returned history, not only turn-end
// snapshots.
func TestAgentKeepsAPruneWhenTheRunEndsEarly(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		backend := &endingBackend{windowBackend: &windowBackend{window: 1 << 30, turns: 50}, failAt: 6}
		if interrupt {
			backend.cancel = cancel
		}
		runner, _ := newCompactionRunner(t, backend.windowBackend, 64_000)
		runner.client = llm.NewClientWithBackend(&llm.Config{}, runner.registry, backend)
		runner.summarize = nil
		st, err := runner.RunGoal(ctx, "read every file")
		cancel()
		if err == nil {
			t.Fatalf("interrupt=%v: the run did not end early", interrupt)
		}
		if runner.budget.CompactCount == 0 {
			t.Fatalf("interrupt=%v: test setup: nothing was pruned", interrupt)
		}
		var sentPruned, keptPruned int
		for _, m := range backend.sent {
			if m.Role == "tool" && len(m.Content) < 1000 {
				sentPruned++
			}
		}
		for _, m := range st.Messages {
			if m.Role == "tool" && len(m.Content) < 1000 {
				keptPruned++
			}
		}
		if sentPruned == 0 || keptPruned != sentPruned {
			t.Fatalf("interrupt=%v: the last request had %d pruned results, the run's history %d", interrupt, sentPruned, keptPruned)
		}
	}
}
