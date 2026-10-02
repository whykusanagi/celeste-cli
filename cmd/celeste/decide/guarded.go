package decide

import (
	"context"
	"fmt"
	"time"
)

// Timeout bounds a model-backed oracle's answer (spec §5 W3: 2.5 s, then
// the heuristic).
const Timeout = 2500 * time.Millisecond

// Guarded asks primary under Timeout. On an error it answers from the
// questions' heuristics, and a question primary left unanswered gets its
// heuristic answer too. It never returns an error. use names the caller in
// the stats ("ballot", "gate", "route", "prune"); logf (nil: none) gets
// one line per failed call. A nil primary is the heuristic alone and is not
// counted in the stats.
func Guarded(primary Oracle, use string, logf func(string)) Oracle {
	return guarded{primary: primary, use: use, logf: logf}
}

type guarded struct {
	primary Oracle
	use     string
	logf    func(string)
}

func (g guarded) Ask(ctx context.Context, state string, qs []Question) (map[string]Answer, error) {
	h, _ := Heuristic{}.Ask(ctx, state, qs)
	if g.primary == nil {
		return h, nil
	}
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	type result struct {
		ans map[string]Answer
		err error
	}
	done := make(chan result, 1)
	go func() {
		a, e := g.primary.Ask(cctx, state, qs)
		done <- result{a, e}
	}()
	var ans map[string]Answer
	var err error
	// The cap holds even for an oracle that ignores its context: the
	// caller never waits past Timeout.
	select {
	case r := <-done:
		ans, err = r.ans, r.err
	case <-cctx.Done():
		err = cctx.Err()
	}
	cancel()
	Record(g.use, time.Since(start), err == nil)
	if err != nil {
		if g.logf != nil {
			g.logf(fmt.Sprintf("oracle (%s): %v; using the heuristic", g.use, err))
		}
		return h, nil
	}
	out := make(map[string]Answer, len(qs))
	for id, a := range h {
		out[id] = a
	}
	for id, a := range ans {
		out[id] = a
	}
	return out, nil
}
