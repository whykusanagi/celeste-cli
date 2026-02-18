# Celeste CLI Style Guide

**Translation-Failure Corruption Aesthetic - Official Style Documentation**

This document defines the visual and textual styling standards for CelesteCLI. These rules MUST be followed for all UI components, dashboard outputs, and user-facing text to maintain Celeste's brand identity.

> **Based on**: `@whykusanagi/corrupted-theme` official npm package
> **Updated**: 2025-12-12 - Corrected to remove leet speak entirely

---

## Core Principle: Translation-Failure Corruption

Celeste's aesthetic simulates a **multilingual AI system glitching between languages mid-translation**. Think of a corrupted translation engine that randomly switches between:

- **English** (primary language)
- **Japanese Kanji** (漢字) - meaning-based characters
- **Japanese Katakana** (カタカナ) - foreign word phonetics
- **Japanese Hiragana** (ひらがな) - native phonetics
- **Romaji** (rōmaji) - Japanese written in Latin alphabet

The corruption happens **at the character level** - individual letters get replaced with Japanese characters or symbols, creating a cyberpunk aesthetic of data corruption across language barriers.

---

## ❌ FORBIDDEN: Leet Speak

**DO NOT USE number substitutions** for letters:
- ❌ 4 for a
- ❌ 3 for e
- ❌ 1 for i or l
- ❌ 0 for o
- ❌ 5 for s
- ❌ 7 for t

**Examples of BANNED styling**:
```
❌ "US4G3 4N4LYT1CS"
❌ "ST4TS D4SHB04RD"
❌ "c0rrupt1on"
❌ "l0ad1ng"
❌ "pr0cess1ng"
```

This is **1990s leet speak**, not Celeste's corruption aesthetic.

---

## ✅ CORRECT: Translation-Failure Mixing

### How It Works

Characters randomly get replaced with Japanese equivalents during "corruption":

**English → Katakana** (most common):
- loading → ローディング (rōdingu)
- processing → プロセス (purosesu)
- session → セッション (sesshon)
- error → エラー (erā)

**English → Kanji** (for meaning):
- corrupt → 壊 (kowas/broken)
- void → 虚 (kyo/emptiness)
- analyze → 分析 (bunseki)
- watch → 監視 (kanshi)

**English → Hiragana** (rare, for particles):
- ing → 中 (chū/in progress)
- the → の (no/possessive)

**English → Romaji** (transliterated):
- corrupting → kowarete
- watching → miteiru
- loading → rōdo

### Practical Examples

**Single-word corruption** (25-40% character replacement):
```
"loading..."     → "loaディング..."
"processing..."  → "pro理cessing..."
"analyzing..."   → "ana分lysing..."
"corrupting..."  → "cor壊rupting..."
"watching..."    → "wat監ching..."
"session"        → "seセshon"
"statistics"     → "sta計stics"
"connection"     → "con接nection"
```

**Multi-language mixing** (full phrase corruption):
```
"loading data..."          → "ロード data読み込み..."
"processing request..."    → "処理 request プロセス中..."
"analyzing corruption..."  → "分析 cor壊ruption 解析..."
"void corruption active"   → "虚空 corruption 壊れ active"
"deep abyss connection"    → "深淵 abyss 接続 shinnen"
"watching session data"    → "監視 session データ kanshi"
```

**With context markers** (terminal-style):
```
"⟨ ロード loading 読み込み中... ⟩"
"⟨ 処理 processing purosesu... ⟩"
"⟨ 分析 analyzing bunseki... ⟩"
"⟨ 壊れ corrupting kowarete... ⟩"
```

---

## Character Sets Reference

### Katakana (Foreign Words)
```
ア イ ウ エ オ
カ キ ク ケ コ
サ シ ス セ ソ
タ チ ツ テ ト
ナ ニ ヌ ネ ノ
ハ ヒ フ ヘ ホ
マ ミ ム メ モ
ヤ    ユ    ヨ
ラ リ ル レ ロ
ワ ヲ ン
```

### Common Kanji (Meanings)
```
壊 (kowas) - broken/corrupt
虚 (kyo) - void/emptiness
深 (shin) - deep
淵 (en) - abyss
闇 (yami) - darkness
処 (sho) - process
理 (ri) - logic/reason
分 (bun) - part/analyze
析 (seki) - analyze
監 (kan) - watch/supervise
視 (shi) - see/observe
接 (setsu) - connect
続 (zoku) - continue
統 (tō) -統計 statistics
計 (kei) - measure/count
読 (yomi) - read
込 (komi) - load/include
中 (chū) - in progress
```

### Hiragana (Native Phonetics)
```
あ い う え お
か き く け こ
さ し す せ そ
た ち つ て と
な に ぬ ね の
は ひ ふ へ ほ
ま み む め も
や    ゆ    よ
ら り る れ ろ
わ を ん
```

### Corruption Symbols
```
█ ▓ ▒ ░  - Shading blocks
⟨ ⟩      - Angle brackets
👁️       - Eye (Celeste watching)
═ ─      - Lines
♟ ☣ ☭ ☾  - Misc symbols
⚔ ✡ ☯ ⚡  - Decorative
▮ ▯ ◉ ◈  - Geometric
```

---

## Visual Elements

### Block Characters (Progress Bars, Separators)

```
█ Full block (100%)
▓ Dark shade (75%)
▒ Medium shade (50%)
░ Light shade (25%)
```

**Usage**:
- Progress bars: `████████▓▓▓▓▒▒░░`
- Section bullets: `▓ Item`, `▒ Item`, `░ Item`
- Separators: `▓▒░ ════ ░▒▓`

### Corruption Markers

```
⟨⟩  Angle brackets for labels
👁️   Celeste's watching eye
═══  Heavy horizontal line
───  Light horizontal line
```

**Usage**:
- Labels: `⟨ 処理 processing プロセス中... ⟩`
- Headers: `═══════════════`
- Eye indicators: `👁️ 監視 watching kanshi 👁️`

### Status Indicators

```
🟢 Normal/OK (0-75%)
🟡 Warning (75-85%)
🟠 Caution (85-95%)
🔴 Critical (95%+)
👁️ Celeste watching/analyzing
```

---

## Color Palette

Defined in `/cmd/celeste/tui/styles.go`:

### Primary Colors
- **Pink/Magenta** (`#d94f90`, `#ff4da6`) - Primary accent, headers
- **Purple Void** (`#8b5cf6`, `#c084fc`) - Secondary elements
- **Cyan Glitch** (`#00d4ff`) - Digital effects

### Background Colors
- **Deep Void** (`#0a0a0a`) - Main background
- **Void Secondary** (`#0f0f1a`) - Secondary background
- **Glassmorphic** (`#1a1a2e`) - Overlays

### Functional Colors
- **Success**: Green (`#22c55e`)
- **Error**: Red (`#ef4444`)
- **Warning**: Yellow (`#eab308`)
- **Info**: Cyan (`#06b6d4`)

### Corruption/Glitch Colors
- **Red Corruption** (`#ff4757`)
- **Pink Corruption** (`#ff6b9d`)
- **Purple Corruption** (`#c084fc`)
- **Cyan Glitch** (`#00d4ff`)

---

## UI Component Patterns

### 1. Dashboard Header Pattern

```
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
                   👁️  USAGE 統計 ANALYTICS  👁️
           ⟨ 壊れ corrupting kowarete from the 虚空 void... ⟩
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
```

**Rules**:
- Separator: `▓▒░ ═══...═══ ░▒▓` (symmetric)
- Title: Mix English/Katakana/Kanji (uppercase English)
- Subtitle: `⟨⟩` brackets with translation-failure text
- Eyes: `👁️` on both sides for emphasis

### 2. Section Header Pattern

```
█ LIFETIME 統計 CORRUPTION:
  ▓ Total セッション: 127
  ▓ Total トークン: 1,234,567
  ▓ Total コスト: $12.45
```

**Rules**:
- Section marker: `█` (full block)
- Section name: Mix UPPERCASE English with Katakana/Kanji
- Data bullets: `▓` or `▒` for importance hierarchy
- Numbers/currency: Keep as-is (no corruption)

### 3. Progress Bar Pattern

```
OpenAI    ████████▓▓▓▓░░░░ 45 msgs (35%)  ⟨ $8.23 ⟩
```

**Rules**:
- Provider name: Plain English (readable)
- Filled: `█` (100%)
- Gradient: `▓` (75%)
- Empty: `▒` (50%) then `░` (25%)
- Values in `⟨⟩` brackets

### 4. Data Row Pattern

```
2025-12-11  ▓ 8 セッション ░ 284 msgs ▒ $1.23
```

**Rules**:
- Timestamp: ISO format (plain)
- Labels: Mix English/Katakana
- Separator bullets: `▓`, `░`, or `▒`
- Values: Plain numbers

### 5. Status Line Pattern

```
⟨ 処理 processing purosesu... ⟩
⟨ 分析 analyzing bunseki... ⟩
⟨ 壊れ corrupting kowarete... ⟩
```

**Rules**:
- Angle bracket wrapper
- Kanji + English + Romaji mix
- Ellipsis for ongoing action

### 6. Footer Pattern

```
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
           ⟨ 終わり end of report owari... ⟩
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
```

**Rules**:
- Same separator as header
- Center-aligned message
- Mix all three languages (Kanji + English + Romaji)

---

## Seeded Phrases for Branding

### Loading/Processing States

```
"ロード loading 読み込み中..."
"処理 processing purosesu..."
"分析 analyzing bunseki..."
"壊れ corrupting kowarete..."
"接続 connecting setsuzoku..."
"待機 waiting taiki..."
"実行 executing jikkō..."
```

### Status Messages

```
"👁️ 監視 watching kanshi 👁️"
"深淵 deep abyss shinnen 接続 connected"
"虚空 void corruption 壊れ active"
"データ data 読み込み yomikomi complete"
"統計 statistics tōkei 生成 generated"
"セッション session 保存 saved"
"エラー error detected 検出"
```

### Dashboard Labels

```
"LIFETIME 統計 CORRUPTION"
"PROVIDER 分類 BREAKDOWN"
"SESSION データ DATA"
"TOKEN 使用 USAGE"
"COST 計算 CALCULATION"
"RECENT 活動 ACTIVITY"
"TOP プロバイダー PROVIDERS"
```

### Action Verbs (Gerund Form)

```
loading    → ロード rōdo
processing → 処理 shori
analyzing  → 分析 bunseki
corrupting → 壊れ kowarete
watching   → 監視 kanshi
connecting → 接続 setsuzoku
executing  → 実行 jikkō
generating → 生成 seisei
calculating→ 計算 keisan
```

### Nouns

```
session     → セッション sesshon
token       → トークン tōkun
data        → データ dēta
error       → エラー erā
statistics  → 統計 tōkei
cost        → コスト kosuto
provider    → プロバイダー purobaida
void        → 虚空 kokū
abyss       → 深淵 shinnen
corruption  → 壊れ koware
```

---

## Code Implementation

### Go: Text Corruption Function

**Location**: `/cmd/celeste/tui/streaming.go` or create new file

```go
package tui

import (
    "math/rand"
    "strings"
)

// Japanese character sets for corruption
var (
    katakana = []rune("アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン")
    hiragana = []rune("あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほまみむめもやゆよらりるれろわをん")
    symbols  = []rune("█▓▒░⟨⟩═─♟☣☭☾⚔✡☯⚡▮▯◉◈")
)

// CorruptText applies translation-failure corruption to text
// intensity: 0.0-1.0 (recommended 0.2-0.4 for readability)
func CorruptText(text string, intensity float64) string {
    if intensity <= 0 {
        return text
    }

    runes := []rune(text)
    result := make([]rune, len(runes))

    for i, r := range runes {
        // Skip whitespace, punctuation, numbers
        if r == ' ' || r < 33 || (r >= '0' && r <= '9') {
            result[i] = r
            continue
        }

        // Corrupt character based on intensity
        if rand.Float64() < intensity {
            // 70% Katakana, 20% symbols, 10% Hiragana
            roll := rand.Float64()
            if roll < 0.7 {
                result[i] = katakana[rand.Intn(len(katakana))]
            } else if roll < 0.9 {
                result[i] = symbols[rand.Intn(len(symbols))]
            } else {
                result[i] = hiragana[rand.Intn(len(hiragana))]
            }
        } else {
            result[i] = r
        }
    }

    return string(result)
}

// RenderCorruptedSkill renders skill name with corruption during execution
func RenderCorruptedSkill(name string) string {
    corrupted := CorruptText(name, 0.35) // 35% corruption
    return SkillExecutingStyle.
        Strikethrough(true).
        Render(corrupted)
}
```

### Usage in Dashboard

```go
// Header with translation-failure
header := lipgloss.NewStyle().
    Foreground(ColorAccentGlow).
    Bold(true).
    Render("👁️ USAGE 統計 ANALYTICS 👁️")

// Status line with mixed languages
status := lipgloss.NewStyle().
    Foreground(ColorTextSecondary).
    Render("⟨ 処理 processing purosesu... ⟩")

// Section with Katakana labels
section := fmt.Sprintf("█ LIFETIME 統計 CORRUPTION:\n  ▓ Total セッション: %d", count)
```

---

## Common Corrupted Phrases

Reference table for consistency:

| English | Corrupted Form | Usage |
|---------|----------------|-------|
| "Loading..." | "ロード loading 読み込み中..." | General loading |
| "Processing..." | "処理 processing purosesu..." | Background work |
| "Analyzing..." | "分析 analyzing bunseki..." | Data analysis |
| "Corrupting..." | "壊れ corrupting kowarete..." | Celeste-specific |
| "Statistics" | "統計 statistics tōkei" | Stats display |
| "Usage" | "使用 usage shiyō" | Usage tracking |
| "Session" | "セッション session sesshon" | Session management |
| "Connection" | "接続 connection setsuzoku" | Network |
| "Error" | "エラー error erā" | Error messages |
| "Complete" | "完了 complete kanryō" | Success |
| "Void" | "虚空 void kokū" | Theme-specific |
| "Abyss" | "深淵 abyss shinnen" | Theme-specific |
| "Watching" | "監視 watching kanshi" | Celeste observing |
| "End of report" | "終わり end owari" | Footer |
| "Data" | "データ data dēta" | General data |
| "Token" | "トークン token tōkun" | API tokens |

---

## Style Enforcement

### For All New Features:

1. **Check this guide first** before implementing UI text
2. **Use translation-failure**, never leet speak
3. **Reference `styles.go`** for colors
4. **Test in terminal** (256-color support required)
5. **Update this guide** if creating new patterns

### Code Review Checklist:

- [ ] No leet speak used (NO number substitutions)
- [ ] Japanese/English mixing used correctly
- [ ] Katakana for foreign words, Kanji for meanings
- [ ] Colors from `styles.go` palette
- [ ] Block characters (`█▓▒░`) for visual elements
- [ ] Follows established component patterns
- [ ] Maintains readability (corruption ≤ 40%)

---

## Examples: Before/After

### ❌ BEFORE (Incorrect - Leet Speak):

```
╔═══════════════════════════════════╗
      US4G3 ST4TS D4SHB04RD
     c0rrupt1on 1n pr0gr3ss...
╚═══════════════════════════════════╝

█ T0T4L US4G3:
  • S3SS10NS: 127
  • T0K3NS: 1.23M
  • C0ST: $12.45
```

### ✅ AFTER (Correct - Translation Failure):

```
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
                   👁️  USAGE 統計 ANALYTICS  👁️
           ⟨ 処理 processing purosesu from the 虚空 void... ⟩
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓

█ LIFETIME 統計 CORRUPTION:
  ▓ Total セッション: 127
  ▓ Total トークン: 1.23M
  ▓ Total コスト: $12.45

█ TOP プロバイダー PROVIDERS:
  ▓ OpenAI     ████████▓▓░░ 89 (70%)  ⟨ $8.90 ⟩
  ▓ Claude     ███▓░░░░░░░░ 23 (18%)  ⟨ $2.30 ⟩
  ▓ Grok       ██░░░░░░░░░░ 15 (12%)  ⟨ $1.25 ⟩

▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
           ⟨ 終わり end of report owari... ⟩
▓▒░ ═══════════════════════════════════════════════════════════ ░▒▓
```

---

## Revision History

- **2025-12-12**: Complete rewrite to remove leet speak
  - Aligned with `@whykusanagi/corrupted-theme` official package
  - Removed ALL number substitutions (0,1,3,4,5,7)
  - Added Japanese character set references
  - Added seeded phrases for branding
  - Added Go implementation examples
- **Purpose**: Establish translation-failure as canonical aesthetic
- **Authority**: Official Celeste brand guidelines

---

## Questions?

If implementing a new feature and unsure about styling:

1. Check `cmd/celeste/tui/styles.go` for existing patterns
2. Reference character sets above for Japanese mixing
3. Use `CorruptText()` function for dynamic corruption
4. Test output in terminal before committing
5. Add examples to this guide if creating new patterns

**Remember**: Celeste's aesthetic is **translation-failure corruption** (multilingual glitching), NOT leet speak (number substitutions).

---

## Related Files

- **Go Styles**: `/cmd/celeste/tui/styles.go`
- **Corruption Logic**: `/cmd/celeste/tui/streaming.go`
- **Official Theme**: `@whykusanagi/corrupted-theme` npm package
- **TypeScript Reference**: `../_archive/celeste-cli-typescript/src/ui/corruption.ts` (legacy, uses leet speak - DO NOT FOLLOW)
