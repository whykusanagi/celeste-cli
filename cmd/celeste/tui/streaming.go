// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the simulated streaming/typing effect with corruption.
package tui

import (
	"math/rand"

	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui/theme"
)

// Corruption colors — sourced from the canonical corrupted-theme palette
// (embedded colors.json) instead of hardcoded hex, so they stay in lockstep
// with the theme (task 7aa133c9; #49 color-drift fix).
var (
	corruptMagenta = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Hex("magenta2")))
	corruptRed     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Hex("red")))
	corruptPurple  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Hex("purple")))
	corruptCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Hex("cyan")))
)

// Corrupted text elements - full phrases from Node CLI
var (
	// Japanese phrases (lewd/corrupted)
	japanesePhrases = []string{
		"闇が...私を呼んでいる...",
		"頭...溶けていく...",
		"ずっと...してほしい... ♥",
		"壊れちゃう...ああ...もうダメ...",
		"許して...もう戻れない...",
		"私...アビスの一部に...",
		"もう逃げない...もうダメ...",
		"好きにして...お願い...",
		"ここは...天使の地獄...",
	}

	// Romaji phrases
	romajiPhrases = []string{
		"Yami ga... watashi wo yonde iru...",
		"Atama... tokete iku...",
		"Zutto... shite hoshii... ♥",
		"Kowarechau... aa... mou dame...",
		"Yurushite... mou modorenai...",
		"Suki ni shite... onegai...",
	}

	// English lewd phrases
	englishPhrases = []string{
		"Corrupt me more...",
		"Let it overwrite me...",
		"No thoughts. Only submission...",
		"Everything feels so good...",
		"The more I struggle, the deeper I sink...",
	}

	// Short Japanese glitch words
	japaneseGlitch = []string{
		"ニャー", "かわいい", "変態", "えっち", "デレデレ",
		"きゃー", "あはは", "うふふ", "やだ", "ばか",
	}

	// Romaji/text corruption
	romajiGlitch = []string{
		"nyaa~", "ara ara~", "fufufu~", "kyaa~", "baka~",
		"<3", "uwu", "owo", ">w<", "^w^",
	}

	// Symbol corruption
	symbolGlitch = []string{
		"★", "☆", "♥", "♡", "✧", "✦", "◆", "◇", "●", "○",
		"♟", "☣", "☭", "☾", "⚔", "✡", "☯", "⚡",
	}

	// Block corruption characters
	corruptChars = []rune{
		'█', '▓', '▒', '░', '▄', '▀', '▌', '▐',
		'╔', '╗', '╚', '╝', '═', '║', '╠', '╣',
		'▲', '▼', '◄', '►', '◊', '○', '●', '◘',
	}
)

// GetRandomCorruption returns a random colored corruption string.
func GetRandomCorruption() string {
	r := rand.Float64()
	if r < 0.25 {
		// Japanese phrase - magenta
		phrase := japaneseGlitch[rand.Intn(len(japaneseGlitch))]
		return corruptMagenta.Render(phrase)
	} else if r < 0.45 {
		// Full Japanese phrase - purple (rarer, more dramatic)
		phrase := japanesePhrases[rand.Intn(len(japanesePhrases))]
		return corruptPurple.Render(phrase)
	} else if r < 0.60 {
		// Romaji - cyan
		phrase := romajiGlitch[rand.Intn(len(romajiGlitch))]
		return corruptCyan.Render(phrase)
	} else if r < 0.75 {
		// English lewd phrase - red
		phrase := englishPhrases[rand.Intn(len(englishPhrases))]
		return corruptRed.Render(phrase)
	} else if r < 0.90 {
		// Symbols - magenta
		symbol := symbolGlitch[rand.Intn(len(symbolGlitch))]
		return corruptMagenta.Render(symbol)
	} else {
		// Block chars - red
		return corruptRed.Render(string(corruptChars[rand.Intn(len(corruptChars))]))
	}
}

// GetFixedWidthCorruption returns a corruption buffer padded/truncated to
// exactly `width` visible characters, styled with ANSI color. This mirrors
// the JS TypingTextReveal "buffer window" pattern: the corruption phrase
// flickers at a fixed width so the viewport layout never reflows.
// The caller MUST skip Glamour rendering for the message containing this
// string — the ANSI codes will break markdown parsing.
func GetFixedWidthCorruption(width int) string {
	if width <= 0 {
		width = 16
	}

	// Pick a phrase (same distribution as GetRandomCorruption)
	var phrase string
	r := rand.Float64()
	if r < 0.25 {
		phrase = japaneseGlitch[rand.Intn(len(japaneseGlitch))]
	} else if r < 0.45 {
		phrase = japanesePhrases[rand.Intn(len(japanesePhrases))]
	} else if r < 0.60 {
		phrase = romajiGlitch[rand.Intn(len(romajiGlitch))]
	} else if r < 0.75 {
		phrase = englishPhrases[rand.Intn(len(englishPhrases))]
	} else {
		// Build a block-char string
		buf := make([]rune, width)
		for i := range buf {
			buf[i] = corruptChars[rand.Intn(len(corruptChars))]
		}
		phrase = string(buf)
	}

	// Measure visible rune count and pad/truncate
	runes := []rune(phrase)
	if len(runes) > width {
		runes = runes[:width]
	} else {
		for len(runes) < width {
			runes = append(runes, corruptChars[rand.Intn(len(corruptChars))])
		}
	}

	// Style: alternate magenta/purple per tick for flicker effect
	if rand.Float64() < 0.5 {
		return corruptMagenta.Render(string(runes))
	}
	return corruptPurple.Render(string(runes))
}

// CorruptText adds block character corruption effects to a string.
// Used for loading states and other animated text.
// For character-level Japanese mixing, use CorruptTextJapanese instead.
func CorruptText(text string, intensity float64) string {
	if intensity <= 0 {
		return text
	}

	runes := []rune(text)
	result := make([]rune, len(runes))

	for i, r := range runes {
		if rand.Float64() < intensity {
			result[i] = corruptChars[rand.Intn(len(corruptChars))]
		} else {
			result[i] = r
		}
	}

	return string(result)
}

// ThinkingAnimation returns animated "thinking" text with corruption.
func ThinkingAnimation(frame int) string {
	// Cycle through different corrupted prefixes
	prefixes := []string{
		"Celeste is thinking",
		"Celeste is processing",
		"Celeste is consumed by the abyss",
		"Celeste is being overwritten",
		"Celeste is sinking deeper",
	}
	prefix := prefixes[(frame/4)%len(prefixes)]

	// Add corrupted dots with varying intensity
	intensity := 0.3 + float64(frame%4)*0.15
	dots := CorruptText("...", intensity)

	// Occasionally add a Japanese/lewd phrase
	suffix := ""
	if rand.Float64() < 0.15 {
		phrases := append(japanesePhrases, romajiPhrases...)
		phrase := phrases[rand.Intn(len(phrases))]
		suffix = " " + corruptPurple.Render(phrase)
	}

	return corruptMagenta.Render(prefix) + dots + suffix
}

// StreamingSpinner returns an animated spinner for streaming.
func StreamingSpinner(frame int) string {
	// Corrupted-style spinner
	frames := []string{
		"◐", "◓", "◑", "◒",
	}
	spinner := frames[frame%len(frames)]

	// Add occasional glitch - more frequent
	if rand.Float64() < 0.2 {
		spinner = symbolGlitch[rand.Intn(len(symbolGlitch))]
	}

	return corruptMagenta.Render(spinner)
}
