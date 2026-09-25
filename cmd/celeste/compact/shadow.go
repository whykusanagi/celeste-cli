package compact

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// maxGoalChars caps the goal and latest message sent to Jev.
const maxGoalChars = 2000

// Shadow wires Jev into opts in shadow mode (#175): Plan keeps its rules
// exactly, and after the prune the caller runs report, which asks Jev and
// logs what it would have elided next to what the rules did. With async the
// call runs in the background (the TUI must not block) and at most one is in
// flight; without it, report returns only after logging, so nothing writes
// to the caller's output after a run ends. A nil client makes report a no-op.
func Shadow(c *jev.Client, msgs []tui.ChatMessage, opts Options, logf func(string), async bool) (Options, func(Result)) {
	var cands []Candidate
	opts.Score = func(cs []Candidate) map[string]float64 {
		cands = cs
		return nil // no opinion: the rules decide
	}
	return opts, func(res Result) {
		rules := map[string]bool{}
		for _, e := range res.Edits {
			if !e.Superseded {
				rules[e.ToolCallID] = true
			}
		}
		// Plan can collect candidates and then abandon the prune: nothing
		// to compare, so nothing is sent.
		if c == nil || len(cands) == 0 || len(rules) == 0 {
			return
		}
		goal, latest := goalAndLatest(msgs)
		run := func() {
			start := time.Now()
			scores, err := JevScore(context.Background(), c, goal, latest, cands)
			line := shadowReport(scores, cands, rules)
			if err != nil {
				line += ": " + err.Error()
			}
			logf(fmt.Sprintf("%s (%v)", line, time.Since(start).Round(time.Millisecond)))
		}
		if !async {
			run()
			return
		}
		if !shadowBusy.CompareAndSwap(false, true) {
			logf("jev shadow: skipped, the previous call is still in flight")
			return
		}
		go func() {
			defer shadowBusy.Store(false)
			run()
		}()
	}
}

// shadowBusy bounds background shadow calls to one at a time.
var shadowBusy atomic.Bool

// shadowReport compares the rules' elisions with the ones Jev would make for
// the same token budget: least-needed first.
func shadowReport(scores map[string]float64, cands []Candidate, rules map[string]bool) string {
	if scores == nil {
		return fmt.Sprintf("jev shadow: no verdict (rules elided %d)", len(rules))
	}
	budget := 0
	for _, c := range cands {
		if rules[c.ToolCallID] {
			budget += c.Tokens
		}
	}
	order := append([]Candidate(nil), cands...)
	need := func(c Candidate) float64 {
		if p, ok := scores[c.ToolCallID]; ok {
			return p
		}
		return 0.5
	}
	sort.SliceStable(order, func(a, b int) bool { return need(order[a]) < need(order[b]) })
	jevElides, freed, agree := map[string]bool{}, 0, 0
	for _, c := range order {
		if freed >= budget {
			break
		}
		jevElides[c.ToolCallID] = true
		freed += c.Tokens
		if rules[c.ToolCallID] {
			agree++
		}
	}
	var kept, dropped []string
	for _, c := range cands {
		desc := fmt.Sprintf("%s(p=%.2f)", describe(c), need(c))
		if rules[c.ToolCallID] && !jevElides[c.ToolCallID] {
			kept = append(kept, desc)
		}
		if jevElides[c.ToolCallID] && !rules[c.ToolCallID] {
			dropped = append(dropped, desc)
		}
	}
	var line string
	if len(rules) == len(cands) {
		line = fmt.Sprintf("jev shadow: no choice, rules elided all %d candidates", len(rules))
	} else {
		line = fmt.Sprintf("jev shadow: rules elided %d of %d, jev would elide %d, agree %d", len(rules), len(cands), len(jevElides), agree)
	}
	if len(kept) > 0 {
		line += "; jev would keep " + strings.Join(kept, ", ")
	}
	if len(dropped) > 0 {
		line += "; jev would elide instead " + strings.Join(dropped, ", ")
	}
	ps := make([]string, len(cands))
	for i, c := range cands {
		ps[i] = fmt.Sprintf("%s=%.2f", describe(c), need(c))
	}
	return line + "; p: " + strings.Join(ps, ", ")
}

// describe names a candidate for the log: the tool and its main argument.
func describe(c Candidate) string {
	for _, k := range []string{"path", "file", "command", "url", "query", "pattern"} {
		if v, ok := c.Args[k].(string); ok && v != "" {
			if len(v) > 60 {
				v = cutRunes(v, 60) + "…"
			}
			return c.Name + " " + jev.Redact(v)
		}
	}
	return c.Name
}

// goalAndLatest returns the first visible user message (the original
// request, or the summary that restates it) and the last one after it.
// Hidden directives are skipped, and latest is empty when there is no later
// request. Both are redacted before they are cut.
func goalAndLatest(msgs []tui.ChatMessage) (goal, latest string) {
	first := -1
	for i, m := range msgs {
		if m.Role != "user" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if h, _ := m.Metadata["hidden"].(bool); h {
			continue
		}
		if first < 0 {
			first, goal = i, m.Content
		} else {
			latest = m.Content
		}
	}
	return cutRunes(jev.Redact(goal), maxGoalChars), cutRunes(jev.Redact(latest), maxGoalChars)
}

// cutRunes cuts s to at most n bytes without splitting a UTF-8 sequence.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
