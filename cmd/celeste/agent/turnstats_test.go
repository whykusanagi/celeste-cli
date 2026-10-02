package agent

import "testing"

// Per-turn progress displays (/agent, /orchestrate lanes) keep a turn's
// stats until a progress event shows them; a reply a stream rule dropped
// must never be shown as the turn (re-review item 5).
func TestKeepTurnStatsSkipsDroppedReplies(t *testing.T) {
	m := map[int]TurnStats{}
	KeepTurnStats(m, TurnStats{Turn: 1, InputTokens: 5, Dropped: true})
	if _, ok := m[1]; ok {
		t.Fatal("a dropped reply was kept as the turn's stats")
	}
	KeepTurnStats(m, TurnStats{Turn: 1, InputTokens: 7})
	KeepTurnStats(m, TurnStats{Turn: 1, InputTokens: 9, Dropped: true})
	if m[1].InputTokens != 7 {
		t.Errorf("turn 1 = %+v, want the shown reply's stats", m[1])
	}
}
