package compact

import (
	"context"
	"fmt"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// WithJev applies jev_prune to a prune's options (#175, 2.0 W3). "on":
// Plan asks Jev for P(needed) and elides the least-needed first (Acting).
// "shadow": the rules decide and the returned report logs Jev's verdict
// (Shadow; async as Shadow describes). Anything else, or a nil client,
// leaves opts alone. Call the report after the prune.
func WithJev(ctx context.Context, c *jev.Client, mode string, msgs []tui.ChatMessage, opts Options, logf func(string), async bool) (Options, func(Result)) {
	noop := func(Result) {}
	if c == nil {
		return opts, noop
	}
	switch mode {
	case "on":
		return Acting(ctx, c, msgs, opts, logf), noop
	case "shadow":
		return Shadow(c, msgs, opts, logf, async)
	}
	return opts, noop
}

// Acting makes Plan ask Jev, on the caller's goroutine (the loop's, never
// the UI's), capped at jev.Timeout. Any error is no opinion: the rules'
// oldest-first order stands. Each call is counted in decide's stats as
// "prune".
func Acting(ctx context.Context, c *jev.Client, msgs []tui.ChatMessage, opts Options, logf func(string)) Options {
	opts.Score = func(cands []Candidate) map[string]float64 {
		goal, latest := goalAndLatest(msgs)
		start := time.Now()
		scores, err := JevScore(ctx, c, goal, latest, cands)
		took := time.Since(start)
		decide.Record("prune", took, err == nil)
		if err != nil {
			logf(fmt.Sprintf("jev prune: %v; oldest first (%v)", err, took.Round(time.Millisecond)))
			return nil
		}
		logf(fmt.Sprintf("jev prune: scored %d of %d candidates (%v)", len(scores), len(cands), took.Round(time.Millisecond)))
		return scores
	}
	return opts
}
