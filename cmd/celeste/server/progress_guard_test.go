package server

import "testing"

// The progress guard must trip when the same tool returns byte-identical results
// for maxNoProgressStreak turns in a row (a stuck loop the args-based guard misses
// when args vary trivially).
func TestProgressGuard_TripsOnIdenticalResults(t *testing.T) {
	var g progressGuard
	tripped := false
	for i := 0; i < maxNoProgressStreak; i++ {
		tripped = g.observe("code_search|no results")
	}
	if !tripped {
		t.Fatalf("expected guard to trip after %d identical results", maxNoProgressStreak)
	}
}

// Distinct results (e.g. each bulk-TTS call writes a different file) must NEVER
// trip the guard, no matter how many calls — this is the bulk-safe property that
// the args-aware guard was built to preserve.
func TestProgressGuard_NeverTripsOnDistinctResults(t *testing.T) {
	var g progressGuard
	for i := 0; i < maxNoProgressStreak*5; i++ {
		sig := "generate_speech|Audio saved: speech_" + string(rune('a'+i%26)) + ".mp3"
		if g.observe(sig) {
			t.Fatalf("guard tripped on distinct results at i=%d — bulk work must not be blocked", i)
		}
	}
}

// A single distinct result mid-streak resets the counter, so a loop must produce
// identical results *consecutively* to trip.
func TestProgressGuard_ResetsOnChange(t *testing.T) {
	var g progressGuard
	for i := 0; i < maxNoProgressStreak-1; i++ {
		g.observe("same")
	}
	if g.observe("different") {
		t.Fatal("a changed result should reset the streak, not trip")
	}
	// After the reset it should take another full run of identical results.
	tripped := false
	for i := 0; i < maxNoProgressStreak; i++ {
		tripped = g.observe("different")
	}
	if !tripped {
		t.Fatal("expected guard to trip after a fresh full streak post-reset")
	}
}

// An empty signature (no tool calls) resets the streak and never trips.
func TestProgressGuard_EmptySigResets(t *testing.T) {
	var g progressGuard
	for i := 0; i < maxNoProgressStreak-1; i++ {
		g.observe("same")
	}
	if g.observe("") {
		t.Fatal("empty signature should not trip")
	}
	if g.streak != 0 {
		t.Fatalf("empty signature should reset streak to 0, got %d", g.streak)
	}
}
