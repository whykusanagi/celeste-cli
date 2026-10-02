package tui

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestCharsPerTickFromTypingSpeed(t *testing.T) {
	for _, tc := range []struct{ speed, want int }{
		{0, 3},   // unset: the default
		{-5, 3},  // invalid: the default
		{60, 3},  // the default speed
		{100, 5}, // 100 chars/sec at 20 ticks/sec
		{20, 1},
		{1, 1},       // slowest still moves
		{100000, 50}, // capped
	} {
		if got := charsPerTickFor(tc.speed); got != tc.want {
			t.Errorf("charsPerTickFor(%d) = %d, want %d", tc.speed, got, tc.want)
		}
	}
}

func TestTypingSpeedRange(t *testing.T) {
	if !ValidTypingSpeed(1) || !ValidTypingSpeed(MaxTypingSpeed) || ValidTypingSpeed(0) || ValidTypingSpeed(MaxTypingSpeed+1) {
		t.Error("ValidTypingSpeed range is 1..MaxTypingSpeed")
	}
}

func typedAfterOneTick(cfg *config.Config, content string) string {
	m := NewApp(nil)
	m.currentSession = nil
	if cfg != nil {
		m = m.SetConfig(cfg)
	}
	m.typingContent = content
	m.streamDone = true
	m.streaming = true
	m.chat = m.chat.AddAssistantMessage("")
	m.chat = m.chat.SetTypingActive(true)
	next, _ := m.Update(TickMsg{})
	return next.(AppModel).typingContent
}

func TestTypingOffShowsTheWholeReplyAtOnce(t *testing.T) {
	content := strings.Repeat("x", 40) + " the end"
	if got := typedAfterOneTick(&config.Config{SimulateTyping: false}, content); got != "" {
		t.Errorf("with simulate_typing off the first tick must finish the reply, typingContent=%q", got)
	}
}

func TestTypingOnAdvancesByTypingSpeed(t *testing.T) {
	content := strings.Repeat("x", 100)
	m := NewApp(nil)
	m.currentSession = nil
	m = m.SetConfig(&config.Config{SimulateTyping: true, TypingSpeed: 100})
	m.typingContent = content
	m.streamDone = true
	m.chat = m.chat.AddAssistantMessage("")
	next, _ := m.Update(TickMsg{})
	if got := next.(AppModel).typingPos; got != 5 {
		t.Errorf("100 chars/sec = 5 per tick, typingPos=%d", got)
	}
}

func TestTypingDefaultsUnchangedWithoutConfig(t *testing.T) {
	m := NewApp(nil)
	m.currentSession = nil
	m.typingContent = strings.Repeat("x", 50)
	m.streamDone = true
	m.chat = m.chat.AddAssistantMessage("")
	next, _ := m.Update(TickMsg{})
	if got := next.(AppModel).typingPos; got != 3 {
		t.Errorf("default is 3 chars per tick, typingPos=%d", got)
	}
}
