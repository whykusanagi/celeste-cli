# Character-Level Corruption

How celeste's corruption effects work in the current code: the typing
animation that reveals a reply behind a flickering corruption buffer, the
status-bar thinking animation, and the corrupted titles in `/stats` and
`/export`. Phrase content and tone live in `CORRUPTION_PHRASES.md` and
`STYLE_GUIDE.md`; this page is about the mechanics.

---

## 1. The typing animation

A reply is not printed in one go. The TUI reveals it a few characters at a
time, and the unrevealed remainder is replaced by a fixed-width block of
corruption that flickers until the text catches up.

**Where:** the typing branch of the tick handler in `cmd/celeste/tui/app.go`,
with the speed math in `cmd/celeste/tui/typing_settings.go` and the buffer in
`cmd/celeste/tui/streaming.go`.

### Timing

| Setting | Meaning |
|---|---|
| Tick | Every 50 ms (20 frames a second), `typingTickInterval`. |
| `typing_speed` | Characters per second. Default 60 (`config.DefaultTypingSpeed`), which is 3 characters per tick. Valid range 1 to 1000. |
| Slowest | One character per tick, so any speed below 20 behaves as 20. |
| Fastest | 50 characters per tick. |
| `simulate_typing: false` | The first tick reveals the whole reply. The commit path and the animation code are the same; only the step size changes. |

With no config loaded (tests), typing is on at the default speed.

### Each tick

1. Advance the reveal position by one step (`typingStep`).
2. Show the revealed text, then a space and `GetFixedWidthCorruption(16)`
   while anything is still hidden.
3. Put `StreamingSpinner` and `ThinkingAnimation` in the status bar.
4. Schedule the next tick. If the reveal has caught up but the stream is
   still open, keep ticking until more text arrives or the stream ends; once
   the stream is done and everything is shown, commit the reply.

While the reply is typing, Glamour markdown rendering is skipped for that
message: the ANSI colour codes in the buffer would break the markdown parser.
The finished reply is rendered normally.

### The corruption buffer: `GetFixedWidthCorruption(width)`

The buffer is always exactly `width` visible characters (16 in the chat), so
the viewport never reflows while it flickers. This is the "buffer window"
pattern from the celeste-tts-bot `TypingTextReveal` component.

Each call picks one source:

| Chance | Source | Examples |
|---|---|---|
| 25% | short Japanese glitch words (`japaneseGlitch`) | ニャー, かわいい, 変態, えっち, デレデレ, きゃー, うふふ, ばか |
| 20% | full Japanese phrases (`japanesePhrases`) | 闇が...私を呼んでいる..., 壊れちゃう...ああ...もうダメ..., ここは...天使の地獄... |
| 15% | romaji glitch (`romajiGlitch`) | nyaa~, ara ara~, fufufu~, uwu, >w< |
| 15% | English phrases (`englishPhrases`) | Corrupt me more..., Let it overwrite me..., The more I struggle, the deeper I sink... |
| 25% | a run of block characters (`corruptChars`) | █▓▒░▄▀▌▐ ╔╗╚╝═║ ▲▼◄►◊○●◘ |

The phrase is cut to `width` runes, or padded to it with random block
characters, then coloured magenta or purple at random, so successive ticks
flicker between the two.

### The status bar: `StreamingSpinner` and `ThinkingAnimation`

- `StreamingSpinner(frame)` cycles ◐ ◓ ◑ ◒, and about one frame in five shows a
  random symbol instead (★ ♥ ✧ ☾ ⚡ ...).
- `ThinkingAnimation(frame)` cycles its prefix every four frames: "Celeste is
  thinking", "... processing", "... consumed by the abyss", "... being
  overwritten", "... sinking deeper". The dots after it go through
  `CorruptText` at an intensity that rises with the frame (0.30 to 0.75), and
  15% of frames append a purple Japanese or romaji phrase ("Yami ga...
  watashi wo yonde iru...", "許して...もう戻れない...").

The phrase pools in `streaming.go` are the animation. Do not remove or water
them down when changing this code.

---

## 2. Character-level Japanese mixing (`/stats`)

`corruptTextCharacterLevel(text, intensity)` in
`cmd/celeste/commands/corruption.go` mixes Japanese characters into English
words, so the text stays readable but glitches mid-word:

```
"USAGE ANALYTICS" -> "US使AGE ANア統LYTICS"
                     "USアGE AN統AL読TICSカ"
                     "USカGE 分NALYTI監S"
```

For each ASCII letter (spaces, punctuation and anything else pass through),
with probability `intensity`:

| Roll | Effect |
|---|---|
| 50% | replace it with a katakana character (ア to ン) |
| 25% | replace it with a kanji fragment (壊虚深淵闇処理分析監視接続統計使用) |
| 25% | keep it, and 30% of the time insert a katakana after it |

The `/stats` header (`renderCorruptedHeader` in `commands/stats.go`) uses it
at 0.35 on "USAGE ANALYTICS", then `corruptTextFlicker`, which on some frames
appends a glitch fragment (エラ, 破, 虚, dat, err, voi ...). The header's eyes
flicker between 👁️, ◉ and ●, and a random stats phrase sits under the title:

```
▓▒░ ═══════════════════════════════════════ ░▒▓
           👁️  US使AGE ANア統LYTICS  👁️
     ⟨ tōkei dēta wo... fuhai sasete iru... ⟩
▓▒░ ═══════════════════════════════════════ ░▒▓
```

Intensity guide for this function:

| Intensity | Look |
|---|---|
| 0.25-0.30 | light: subsection titles |
| 0.30-0.35 | medium: section and dashboard titles (the `/stats` title uses 0.35) |
| 0.35-0.40 | heavy |

This is the style the guide asks for: "loaディング", "pro理cessing",
"ana分lysing", "cor壊rupting", "sta計stics".

---

## 3. Word-level contextual corruption (`/export`)

`corruptTextSimple(text, intensity)` in the same file works on whole words.
With probability `intensity` a word is replaced by a fragment chosen for its
meaning: data words (usage, stat, token, cost, session ...) get dēta, 統計,
tōkei, 解析; system words get shisutemu, 処理, 実行; status words get 状態,
shinkō, 完了; time words get kioku, 記憶, 過去, 永遠; void words get 深淵,
虚無, 崩壊, 腐敗, oshiete. Anything else keeps its first half plus a glitch
fragment. Otherwise, a word may get a fragment appended.

`/export` uses it at 0.30 on the format name in its "Exporting to ..." line.

---

## 4. Block corruption: `CorruptText`

`tui.CorruptText(text, intensity)` replaces each character, with probability
`intensity`, by a block or box-drawing character (█▓▒░ ...). It is hard to
read on purpose. Today it corrupts the dots of `ThinkingAnimation`.

`tui.GetRandomCorruption()` draws one coloured item from the same pools as
the buffer (with a symbol source in place of the block run). Nothing calls it
at the moment; it is kept for the animation's use.

---

## Choosing an effect

| Context | Function | Intensity |
|---|---|---|
| Reply being typed | `GetFixedWidthCorruption(16)` after the revealed text | n/a |
| Status bar while working | `StreamingSpinner` + `ThinkingAnimation` | rises per frame |
| Dashboard or section title | `corruptTextCharacterLevel` (+ `corruptTextFlicker`) | 0.25-0.35 |
| Short label in a command's output | `corruptTextSimple` | 0.30 |
| Dramatic, unreadable glitch | `CorruptText` | 0.30-0.50 |
| Status phrases | none: use the phrase bank (`CORRUPTION_PHRASES.md`) | n/a |

## Related

- `cmd/celeste/tui/streaming.go`: phrase pools, buffer, spinner, thinking animation
- `cmd/celeste/tui/typing_settings.go`: `typing_speed` to characters per tick
- `cmd/celeste/commands/corruption.go`: character- and word-level title corruption
- `cmd/celeste/commands/stats.go`: the `/stats` header and footer
- `docs/STYLE_GUIDE.md`, `docs/CORRUPTION_PHRASES.md`
