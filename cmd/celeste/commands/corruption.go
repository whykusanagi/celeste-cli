package commands

import (
	"math/rand"
	"strings"
)

// Color constants shared across commands (avoiding import cycle with tui)
const (
	colorPink       = "#d94f90"
	colorPurpleNeon = "#c084fc"
	colorCyan       = "#00d4ff"
)

// Contextual corruption fragments - romanji, incomplete kanji, cyberpunk terms
// These make semantic sense in context of stats/analytics/data
var (
	// Data/analytics related corruption
	dataCorruption = []string{
		"dēta", "デー", "情報", "jōhō", "統計", "tōkei", "数値", "sūchi",
		"kaiseki", "解析", "kei", "測定", "sokutei", "kiroku", "記録",
	}

	// System/technical corruption
	systemCorruption = []string{
		"shisutemu", "システ", "処理", "shori", "jikkou", "実行", "sōsa", "操作",
		"seigyo", "制御", "kanri", "管理", "dendō", "伝導",
	}

	// State/status corruption
	statusCorruption = []string{
		"jōtai", "状態", "sutēta", "ステ", "reberu", "レベ", "shinkō", "進行",
		"kanryō", "完了", "shori-chū", "処理中", "taiki", "待機",
	}

	// Existential/void corruption (Celeste theme)
	voidCorruption = []string{
		"shin'en", "深淵", "kyomu", "虚無", "konton", "混沌", "zetsubō", "絶望",
		"shōmetsu", "消滅", "hōkai", "崩壊", "fuhai", "腐敗", "oshiete", "教えて",
	}

	// Memory/time corruption
	memoryCorruption = []string{
		"kioku", "記憶", "wasureru", "忘れ", "kako", "過去", "genzai", "現在",
		"mirai", "未来", "toki", "時", "eien", "永遠", "ichiji", "一時",
	}

	// Glitch fragments - incomplete words/characters
	glitchFragments = []string{
		"エラ", "デー", "破", "消", "記", "忘", "混", "虚", "深", "崩",
		"dat", "err", "cor", "del", "mem", "voi", "cha", "sys",
	}
)

// corruptTextSimple creates contextual language corruption
// Uses romanji, incomplete kanji, and context-appropriate glitches
func corruptTextSimple(text string, intensity float64) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	result := make([]string, len(words))
	for i, word := range words {
		lowerWord := strings.ToLower(word)

		// Context-aware corruption based on word meaning
		if rand.Float64() < intensity {
			var replacement string

			// Choose contextually appropriate corruption
			switch {
			case containsAny(lowerWord, []string{"data", "usage", "stat", "analytic", "metric", "token", "count"}):
				replacement = dataCorruption[rand.Intn(len(dataCorruption))]
			case containsAny(lowerWord, []string{"system", "process", "execute", "operation", "control"}):
				replacement = systemCorruption[rand.Intn(len(systemCorruption))]
			case containsAny(lowerWord, []string{"status", "state", "level", "progress", "complete"}):
				replacement = statusCorruption[rand.Intn(len(statusCorruption))]
			case containsAny(lowerWord, []string{"cost", "session", "provider", "model"}):
				replacement = dataCorruption[rand.Intn(len(dataCorruption))]
			case containsAny(lowerWord, []string{"time", "day", "week", "history", "past"}):
				replacement = memoryCorruption[rand.Intn(len(memoryCorruption))]
			case containsAny(lowerWord, []string{"void", "abyss", "corrupt", "consume", "decay"}):
				replacement = voidCorruption[rand.Intn(len(voidCorruption))]
			default:
				// Generic corruption - use glitch fragments
				if len(word) > 4 {
					// Partial word corruption
					fragment := word[:len(word)/2]
					glitch := glitchFragments[rand.Intn(len(glitchFragments))]
					replacement = fragment + glitch
				} else {
					replacement = glitchFragments[rand.Intn(len(glitchFragments))]
				}
			}

			result[i] = replacement
		} else if rand.Float64() < intensity*0.4 {
			// Subtle corruption: append fragment
			if len(word) > 3 {
				glitch := glitchFragments[rand.Intn(len(glitchFragments))]
				result[i] = word + glitch
			} else {
				result[i] = word
			}
		} else {
			result[i] = word
		}
	}

	return strings.Join(result, " ")
}

// containsAny checks if text contains any of the given substrings
func containsAny(text string, substrings []string) bool {
	for _, sub := range substrings {
		if strings.Contains(text, sub) {
			return true
		}
	}
	return false
}

// corruptTextCharacterLevel mixes Japanese characters INTO English words
// This creates the classic translation-failure aesthetic: "loaディング", "pro理cessing"
func corruptTextCharacterLevel(text string, intensity float64) string {
	if intensity <= 0 {
		return text
	}

	// Japanese character sets for character-level mixing
	katakana := []rune("アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン")
	kanjiFragments := []rune("壊虚深淵闇処理分析監視接続統計使用")

	runes := []rune(text)
	result := make([]rune, 0, len(runes)*2) // Pre-allocate extra space for insertions

	for i, r := range runes {
		// Skip spaces and punctuation
		if r == ' ' || r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			result = append(result, r)
			continue
		}

		// Decide corruption type based on intensity
		if rand.Float64() < intensity {
			roll := rand.Float64()

			if roll < 0.5 {
				// Replace character with Katakana (50%)
				result = append(result, katakana[rand.Intn(len(katakana))])
			} else if roll < 0.75 {
				// Replace with Kanji fragment (25%)
				result = append(result, kanjiFragments[rand.Intn(len(kanjiFragments))])
			} else {
				// Keep original and maybe insert after (25%)
				result = append(result, r)
				if i < len(runes)-1 && rand.Float64() < 0.3 {
					// Insert Katakana after
					result = append(result, katakana[rand.Intn(len(katakana))])
				}
			}
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}

// corruptTextFlicker adds flickering corruption (like Celeste's animation)
// Returns text with random corruption artifacts that appear/disappear
func corruptTextFlicker(text string, frame int) string {
	// Flicker intensity based on frame
	flickerIntensity := 0.1 + float64(frame%4)*0.05

	if rand.Float64() < flickerIntensity {
		// Add trailing glitch
		glitch := glitchFragments[rand.Intn(len(glitchFragments))]
		return text + " " + glitch
	}

	return text
}
