package compact

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

func msg(role, content string) tui.ChatMessage { return tui.ChatMessage{Role: role, Content: content} }

func TestMeterBeforeTheFirstRequestEverythingIsSeen(t *testing.T) {
	m := NewMeter(1_000)
	h := []tui.ChatMessage{msg("user", "hi"), msg("assistant", "hello"), msg("user", "again")}
	m.Observe(h, 0)
	if got := m.Unseen(h); got != 0 {
		t.Fatalf("Unseen = %d, want 0 before any request", got)
	}
	if got, want := m.Used(h), Estimate(h)+1_000; got != want {
		t.Fatalf("Used = %d, want estimate + overhead %d", got, want)
	}
}

// Ruling 1: a request counts as seen once its reply is in the history.
func TestMeterUnseenIsWhatCameAfterTheLastAnsweredRequest(t *testing.T) {
	m := NewMeter(0)
	h := []tui.ChatMessage{msg("user", "read")}
	m.Observe(h, 0)
	m.Sending(h) // request 1 carries 1 message
	h = append(h,
		tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "a", Name: "read_file"}}},
		tui.ChatMessage{Role: "tool", ToolCallID: "a", Content: "body"})
	m.Observe(h, 500)
	if got := m.Unseen(h); got != 2 {
		t.Fatalf("Unseen = %d, want 2 (the reply and its result)", got)
	}
	m.Sending(h) // request 2 fails: no reply appended
	m.Observe(h, 0)
	if got := m.Unseen(h); got != 2 {
		t.Fatalf("after a failed request Unseen = %d, want still 2", got)
	}
}

// Ruling 2 / #234 item 1: the last prompt count predates the results just
// appended, and the history estimate alone misses the system prompt.
func TestMeterUsedCountsOverheadAndAppended(t *testing.T) {
	m := NewMeter(11_000)
	h := []tui.ChatMessage{msg("user", "read")}
	m.Observe(h, 0)
	m.Sending(h)
	h = append(h,
		tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "a", Name: "read_file"}}},
		tui.ChatMessage{Role: "tool", ToolCallID: "a", Content: strings.Repeat("x", 80_000)})
	m.Observe(h, 12_000) // the provider's count for request 1
	appended := Estimate(h[1:])
	if got, want := m.Used(h), 12_000+appended; got != want {
		t.Fatalf("Used = %d, want prompt + appended = %d", got, want)
	}
	m2 := NewMeter(11_000)
	m2.Observe(h, 0)
	if got, want := m2.Used(h), Estimate(h)+11_000; got != want {
		t.Fatalf("with no provider count Used = %d, want estimate + overhead %d", got, want)
	}
}

// A reply whose provider reported no usage still marks its request as
// seen, but must not wipe the last real prompt count: Used keeps that
// count and adds everything appended after the request it measured.
func TestMeterZeroUsageReplyKeepsTheProviderBaseline(t *testing.T) {
	m := NewMeter(0)
	h := []tui.ChatMessage{msg("user", "read")}
	m.Observe(h, 0)
	m.Sending(h) // request 1 carries 1 message
	h = append(h,
		tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "a", Name: "read_file"}}},
		tui.ChatMessage{Role: "tool", ToolCallID: "a", Content: "body"})
	m.Observe(h, 50_000) // provider counted request 1
	m.Sending(h)         // request 2 carries 3 messages
	h = append(h,
		tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "b", Name: "read_file"}}},
		tui.ChatMessage{Role: "tool", ToolCallID: "b", Content: strings.Repeat("y", 4_000)})
	m.Observe(h, 0) // request 2 answered, but the reply carried no usage
	if got := m.Unseen(h); got != 2 {
		t.Fatalf("Unseen = %d, want 2 (request 2 was answered)", got)
	}
	if got, want := m.Used(h), 50_000+Estimate(h[1:]); got != want {
		t.Fatalf("Used = %d, want the request-1 baseline plus everything after it = %d", got, want)
	}
}

// A prune or summary shrinks the history without changing its length (or
// shortens it); Used must fall with it, not keep reporting the pre-prune
// prompt count until the provider reports a new one. The meter keeps the
// provider's surplus over the estimate, not the absolute count.
func TestMeterUsedFallsAfterAPruneWithoutNewUsage(t *testing.T) {
	m := NewMeter(1_000)
	h := []tui.ChatMessage{
		msg("user", "read"),
		{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "a", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "a", Content: strings.Repeat("x", 80_000)},
	}
	m.Observe(h, 0)
	m.Sending(h) // request 1 carries the read
	h = append(h, tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "b", Name: "todo"}}},
		tui.ChatMessage{Role: "tool", ToolCallID: "b", Content: "ok"})
	surplus := 2_000
	m.Observe(h, Estimate(h[:3])+surplus) // the provider counted request 1
	before := m.Used(h)
	// The prune elides the read: same length, far fewer tokens.
	h[2].Content = "[elided]"
	if got, want := m.Used(h), Estimate(h)+surplus; got != want {
		t.Fatalf("Used right after the prune = %d, want estimate + surplus = %d (was %d)", got, want, before)
	}
	m.Sending(h)
	h = append(h, msg("assistant", "done"))
	m.Observe(h, 0) // request 2 answered without usage
	if got, want := m.Used(h), Estimate(h)+surplus; got != want {
		t.Fatalf("Used after a zero-usage reply = %d, want estimate + the provider's surplus = %d (pre-prune %d)", got, want, before)
	}
}

func TestDefinitionTokens(t *testing.T) {
	defs := []tui.SkillDefinition{{Name: "read_file", Description: strings.Repeat("d", 400)}}
	if got := DefinitionTokens(defs); got < 100 {
		t.Fatalf("DefinitionTokens = %d, want at least the description's 100", got)
	}
	if DefinitionTokens(nil) != 0 {
		t.Fatal("no tools cost nothing")
	}
}
