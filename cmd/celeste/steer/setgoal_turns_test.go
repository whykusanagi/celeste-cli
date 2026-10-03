package steer

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
)

// A new goal starts a new watchdog history: the same tool call under the
// old prompt and then under the new one is not a loop.
func TestSetGoalStartsANewTurnHistory(t *testing.T) {
	s := New(Options{Watchdog: "on", Goal: "read a", Every: 1})
	turn(s, nil)
	s.Wait()
	s.SetGoal("now read a again, for a different reason")
	turn(s, nil)
	s.Wait()
	for _, b := range []loop.Boundary{loop.BoundaryTools, loop.BoundaryRun, loop.BoundaryRetry} {
		for _, r := range s.Reminders(b) {
			t.Errorf("a turn under the old goal steered the new one: %+v", r)
		}
	}
	s.mu.Lock()
	n := len(s.turns)
	s.mu.Unlock()
	if n != 1 {
		t.Errorf("turns = %d after the goal changed, want only the new goal's 1", n)
	}

	// Under one goal the same call twice is still a loop.
	s2 := New(Options{Watchdog: "on", Goal: "read a", Every: 1})
	turn(s2, nil)
	s2.Wait()
	turn(s2, nil)
	s2.Wait()
	got := 0
	for _, b := range []loop.Boundary{loop.BoundaryTools, loop.BoundaryRun, loop.BoundaryRetry} {
		got += len(s2.Reminders(b))
	}
	if got == 0 {
		t.Error("the same call twice under one goal was not flagged as a loop")
	}
}
