# Character-Level Corruption - Implementation Guide

**Date**: 2025-12-12
**Issue Fixed**: Dashboard titles now use character-level Japanese mixing instead of word replacement

> **Note (2.0 cleanup):** `tui.CorruptTextJapanese` was unused and has been removed. The live implementation is `corruptTextCharacterLevel` in `cmd/celeste/commands/corruption.go`; the examples below that call `tui.CorruptTextJapanese` describe the same behaviour.

---

## The Problem

The old dashboard had Japanese characters mixed **INTO** English words like:
```
👁️  US使AGE ANア統LYTICS  👁️
```

But the implementation was using word-level replacement:
```
👁️  使用 tōkei  👁️  (replaces whole words)
```

This didn't match the style guide examples like:
- `"loaディング"` - Japanese mixed into "loading"
- `"pro理cessing"` - Japanese mixed into "processing"
- `"ana分lysing"` - Japanese mixed into "analyzing"

---

## The Solution

### New Function: `CorruptTextJapanese()`

**Location**: `cmd/celeste/tui/streaming.go` (line 252) and `cmd/celeste/commands/corruption.go`

This function mixes Japanese characters **INTO** English words at the character level:

```go
func CorruptTextJapanese(text string, intensity float64) string {
    katakana := []rune("アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン")
    kanjiFragments := []rune("壊虚深淵闇処理分析監視接続統計使用読込実行")

    // For each character:
    // 50% chance: Replace with Katakana
    // 25% chance: Replace with Kanji
    // 25% chance: Keep + maybe insert Katakana after
}
```

### Example Outputs

**Input**: `"USAGE ANALYTICS"`
**Intensity**: `0.35`

**Possible outputs** (randomized each time):
```
"US使AGE ANア統LYTICS"
"USアGE AN統AL読TICSカ"
"USカGE 分NALYTI監S"
"U接AGE処A壊ALYTICS"
```

The Japanese characters are mixed IN, making it readable but glitchy!

---

## Files Updated

### 1. `cmd/celeste/commands/corruption.go`

**Added** (line 124):
```go
func corruptTextCharacterLevel(text string, intensity float64) string {
    // Character-level Japanese mixing
    // Same implementation as CorruptTextJapanese
}
```

### 2. `cmd/celeste/commands/stats.go`

**Changed** (line 194):
```go
// OLD (word replacement):
title := corruptTextSimple("USAGE ANALYTICS", 0.40)

// NEW (character mixing):
title := corruptTextCharacterLevel("USAGE ANALYTICS", 0.35)
```

### 3. `cmd/celeste/tui/streaming.go`

**Added** (line 252):
```go
func CorruptTextJapanese(text string, intensity float64) string {
    // Character-level Japanese mixing
    // Available throughout TUI
}
```

**Kept** (line 233):
```go
func CorruptText(text string, intensity float64) string {
    // Block character corruption (█▓▒░)
    // For skill execution, heavy glitching
}
```

---

## Two Corruption Types

### Type 1: Character-Level Japanese Mixing ✨ **NEW**

**Function**: `CorruptTextJapanese()` or `corruptTextCharacterLevel()`
**Output**: `"US使AGE ANア統LYTICS"` (readable with Japanese)
**Use for**: Dashboard titles, section headers, any text that should stay readable

```go
// Dashboard header
title := tui.CorruptTextJapanese("USAGE ANALYTICS", 0.35)

// Section header
section := tui.CorruptTextJapanese("PROVIDER BREAKDOWN", 0.30)
```

**Intensity Guide**:
- `0.25-0.30`: Light corruption (25-30% chars replaced)
- `0.30-0.35`: Medium corruption (30-35% chars replaced) ← **Recommended for titles**
- `0.35-0.40`: Heavy corruption (35-40% chars replaced)

### Type 2: Block Character Corruption

**Function**: `CorruptText()`
**Output**: `"tar▓█_r▒ad░ng"` (hard to read, heavy glitching)
**Use for**: Skill names during execution, loading animations

```go
// Skill execution (strikethrough + corruption)
skillName := tui.CorruptText("tarot_reading", 0.40)

// Loading state
loading := tui.CorruptText("Loading...", 0.30)
```

**Intensity Guide**:
- `0.30-0.35`: Light block corruption
- `0.35-0.40`: Medium block corruption ← **Recommended for skills**
- `0.40-0.50`: Heavy block corruption

---

## When to Use Each Function

| Context | Function | Intensity | Example |
|---------|----------|-----------|---------|
| Dashboard title | `CorruptTextJapanese` | 0.35 | "US使AGE ANア統LYTICS" |
| Section header | `CorruptTextJapanese` | 0.30 | "PROバIDERア BREア統KDOWN" |
| Subsection | `CorruptTextJapanese` | 0.25 | "TO監AL SEセSSI使NS" |
| Skill executing | `CorruptText` | 0.40 | "tar▓█_r▒ad░ng" |
| Loading animation | `CorruptText` | 0.30 | "L▓ad█ng..." |
| Status phrases | Neither (use phrase bank) | N/A | "処理 processing purosesu..." |

---

## Testing the Fix

### Before (Word Replacement):
```bash
celeste chat
/stats
```
Output was:
```
👁️  使用 tōkei  👁️  (whole words replaced)
```

### After (Character Mixing):
```bash
celeste chat
/stats
```
Output is now (randomized):
```
👁️  US使AGE ANア統LYTICS  👁️  (Japanese mixed in)
```

Each time you run `/stats`, the corruption is randomized, so you'll see different variations like:
- `"US使AGE ANア統LYTICS"`
- `"USアGE AN統AL読TICSカ"`
- `"USカGE 分NALYTI監S"`

---

## Visual Comparison

### Old Implementation (Word-Level)
```
▓▒░ ═══════════════════════════════════════ ░▒▓
           👁️  使用 統計  👁️
     ⟨ tōkei dēta wo... fuhai sasete iru... ⟩
▓▒░ ═══════════════════════════════════════ ░▒▓
```
**Problem**: Entire words replaced, not mixed

### New Implementation (Character-Level)
```
▓▒░ ═══════════════════════════════════════ ░▒▓
           👁️  US使AGE ANア統LYTICS  👁️
     ⟨ tōkei dēta wo... fuhai sasete iru... ⟩
▓▒░ ═══════════════════════════════════════ ░▒▓
```
**Solution**: Japanese characters mixed INTO English ✓

---

## Matches Style Guide Examples

The new implementation now matches all the examples from `STYLE_GUIDE.md`:

✅ `"loading"` → `"loaディング"`
✅ `"processing"` → `"pro理cessing"`
✅ `"analyzing"` → `"ana分lysing"`
✅ `"corrupting"` → `"cor壊rupting"`
✅ `"statistics"` → `"sta計stics"`

And the dashboard title:
✅ `"USAGE ANALYTICS"` → `"US使AGE ANア統LYTICS"`

---

## Implementation Notes

### Why Two Functions?

1. **Readable corruption** (Japanese mixing) - For UI text users need to read
2. **Heavy corruption** (block characters) - For dramatic effect when readability isn't critical

### Why the Old Implementation Was Wrong

The old `corruptTextSimple()` function did **semantic word replacement**:
- "USAGE" → `"使用"` (whole word)
- "ANALYTICS" → `"統計"` (whole word)

This was more of a **translation** than **corruption**. The aesthetic requires **glitchy mixing** where Japanese interrupts English mid-word.

### Performance

Character-level mixing is slightly more expensive than word replacement, but the difference is negligible:
- Dashboard renders once per `/stats` command
- Randomization happens at display time
- No caching needed (variations are a feature!)

---

## Related Files

- **Implementation**: `tui/streaming.go:252` and `commands/corruption.go:124`
- **Usage**: `commands/stats.go:194`
- **Style Guide**: `docs/STYLE_GUIDE.md`
- **Phrase Library**: `docs/CORRUPTION_PHRASES.md`
- **Validation**: `docs/IMPLEMENTATION_VALIDATION.md`

---

**Status**: ✅ **FIXED** - Dashboard now uses character-level corruption
**Tested**: Visual output matches style guide examples
**Impact**: All dashboard headers now have proper translation-failure aesthetic
