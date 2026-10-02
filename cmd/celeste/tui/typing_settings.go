package tui

import "time"

// typing_speed is in characters per second, as documented. The animation
// advances once per typingTickInterval, so a speed maps to chars per tick.
const (
	// DefaultTypingSpeed is 3 characters per 50ms tick.
	DefaultTypingSpeed = 60
	// MaxTypingSpeed bounds typing_speed (1..MaxTypingSpeed).
	MaxTypingSpeed  = 1000
	maxCharsPerTick = 50
)

// ValidTypingSpeed reports whether n is an acceptable typing_speed.
func ValidTypingSpeed(n int) bool { return n >= 1 && n <= MaxTypingSpeed }

// The slowest the animation goes is one character per tick, i.e. 20 chars/sec:
// a typing_speed below 20 behaves as 20 (documented in the README).
//
// charsPerTickFor converts a typing_speed to characters per tick; an unset
// or invalid speed gets the default, and the slowest speed still advances.
func charsPerTickFor(speed int) int {
	if speed < 1 {
		speed = DefaultTypingSpeed
	}
	perTick := (speed*int(typingTickInterval/time.Millisecond) + 500) / 1000
	if perTick < 1 {
		perTick = 1
	}
	if perTick > maxCharsPerTick {
		perTick = maxCharsPerTick
	}
	return perTick
}

// typingStep is how many characters one tick reveals. With simulate_typing
// off it is the whole reply, so the first tick finishes it (the commit path
// and the corruption animation code are otherwise unchanged). Without a
// config (tests) typing is on at the default speed.
func (m AppModel) typingStep() int {
	if m.config == nil {
		return charsPerTickFor(DefaultTypingSpeed)
	}
	if !m.config.SimulateTyping {
		return len(m.typingContent)
	}
	return charsPerTickFor(m.config.TypingSpeed)
}
