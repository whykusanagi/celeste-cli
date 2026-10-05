package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ttyReader feeds text to the input the way Bubble Tea v1's reader
// (readAnsiInputs) does on a terminal without bracketed paste: reads of
// at most 256 bytes, each parsed into a rune run per word, a space key
// per space and Enter per CR; and when a read fills the buffer and ends
// inside a word, that word is held back and parsed with the next read
// (key.go: canHaveMoreData), which may be a key typed seconds later.
type ttyReader struct{ held string }

const ttyReadSize = 256

// read is one read of b: the messages it yields.
func (r *ttyReader) read(b string) []tea.KeyMsg {
	full := len(b) == ttyReadSize
	b = r.held + b
	r.held = ""
	var out []tea.KeyMsg
	for i := 0; i < len(b); {
		switch b[i] {
		case ' ':
			out = append(out, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			i++
			continue
		case '\r':
			out = append(out, tea.KeyMsg{Type: tea.KeyEnter})
			i++
			continue
		}
		j := i
		for j < len(b) && b[j] != ' ' && b[j] != '\r' {
			j++
		}
		if j == len(b) && full {
			r.held = b[i:]
			break
		}
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(b[i:j])})
		i = j
	}
	return out
}

// paste reads s in full-size reads, the last one short unless s is a
// multiple of the read size.
func (r *ttyReader) paste(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for len(s) > 0 {
		n := min(ttyReadSize, len(s))
		out = append(out, r.read(s[:n])...)
		s = s[n:]
	}
	return out
}

// rowPaste is the K3 burst: "row NNNNN 0123…" words, exactly n bytes, and
// never a space at a read boundary, so a paste of a multiple of 256 bytes
// ends inside a word.
func rowPaste(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "row %05d 0123456789abcdef0123456789abcdef ", i)
	}
	s := b.String()[:n]
	if s[n-1] == ' ' {
		s = s[:n-1] + "x"
	}
	return s
}

// settleTicks runs the input's commands and feeds back the tail's settle
// tick, as the program would once it fires.
func settleTicks(t *testing.T, m InputModel, cmd tea.Cmd) InputModel {
	t.Helper()
	for _, msg := range collectMsgs(cmd) {
		if s, ok := msg.(tailSettleMsg); ok {
			m, _ = m.Update(s)
		}
	}
	return m
}

// tailInput is an input at the 8,192-token window (a 32,768-character
// limit) holding "BASE ", after an unbracketed burst of paste that
// overflowed it, two seconds later. It returns the reader, which may hold
// the paste's last word.
func tailInput(t *testing.T, move func(time.Duration), paste string) (InputModel, *ttyReader) {
	t.Helper()
	m := NewInputModel().Focus().SetContextWindow(8192)
	require.Equal(t, 32_768, m.CharLimit())
	m = typeInput(m, "BASE ")
	move(time.Second)
	r := &ttyReader{}
	for _, k := range r.paste(paste) {
		m, _ = m.Update(k)
	}
	require.Equal(t, "BASE ", m.Value(), "the paste is rolled back")
	require.Contains(t, m.Notice(), "not inserted")
	move(2 * time.Second)
	return m, r
}

// K3: the last word of an over-limit unbracketed paste, held by the
// reader until the next read, never lands in the input; the key typed
// after it does.
func TestInputOverflowHeldTailDropped(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(40_960))
	require.NotEmpty(t, r.held, "the burst must end on a full read inside a word")

	var cmd tea.Cmd
	for _, k := range r.read("x") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE x", m.Value())

	move(time.Second)
	m = typeInput(m, "yz")
	assert.Equal(t, "BASE xyz", m.Value())
}

// K3: the held word followed in the same read by Enter is dropped, and
// Enter sends the input as it was.
func TestInputOverflowHeldTailThenEnter(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(40_960))
	require.NotEmpty(t, r.held)

	var sent tea.Cmd
	for _, k := range r.read("\r") {
		m, sent = m.Update(k)
	}
	msg, ok := queuedSend(sent)
	require.True(t, ok, "Enter sends")
	assert.Equal(t, "BASE ", msg.Content)
}

// K3: the held word followed in the same read by a space is dropped; the
// space is kept.
func TestInputOverflowHeldTailThenSpace(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(40_960))
	require.NotEmpty(t, r.held)

	var cmd tea.Cmd
	for _, k := range r.read(" ") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE  ", m.Value())
}

// A burst that ends on a short read leaves nothing held: the next key is
// typing, inserted at once.
func TestInputOverflowShortTailTypingKept(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(41_000)+" ZZTAIL")
	require.Empty(t, r.held)

	var cmd tea.Cmd
	for _, k := range r.read("x") {
		m, cmd = m.Update(k)
	}
	assert.Equal(t, "BASE x", m.Value(), "typing after the burst is inserted at once")
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE x", m.Value())
}

// A paste that ends on a space leaves nothing held either; keys typed
// after it, one per read, are all kept, slow or fast.
func TestInputOverflowTypingAfterSpaceKept(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(41_000)+" ")
	require.Empty(t, r.held)

	var cmd tea.Cmd
	for _, k := range r.read("a") {
		m, cmd = m.Update(k)
	}
	move(20 * time.Millisecond) // fast typing: inside the burst window
	for _, k := range r.read("b") {
		m, _ = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	move(time.Second)
	for _, k := range r.read("c") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE abc", m.Value())
}

// K3 through the app, at both audit sizes: after the held word and one
// typed key, the input row shows only what the user typed.
func TestAppOverflowHeldTailView(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			move := stoppedKeyClock(t)
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m.input = m.input.SetContextWindow(8192)
			for _, r := range "BASE " {
				m, _ = step(t, m, runeKeyOrSpace(r))
			}
			move(time.Second)
			r := &ttyReader{}
			for _, k := range r.paste(rowPaste(40_960)) {
				m, _ = step(t, m, k)
			}
			require.NotEmpty(t, r.held)
			move(2 * time.Second)
			var cmd tea.Cmd
			for _, k := range r.read("x") {
				m, cmd = step(t, m, k)
			}
			for _, msg := range collectMsgs(cmd) {
				if s, ok := msg.(tailSettleMsg); ok {
					m, _ = step(t, m, s)
				}
			}
			assert.Equal(t, "BASE x", m.input.Value())
			m, _ = step(t, m, inputRedrawMsg{})
			frame := auditView(m)
			assert.Contains(t, frame, "❯ BASE x", frame)
			assert.NotContains(t, frame, "0123x")
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}

func runeKeyOrSpace(r rune) tea.KeyMsg {
	if r == ' ' {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return runeKey(r)
}

// A new unbracketed paste right after an overflowed one that ended on a
// space lands whole, its first word included.
func TestInputOverflowThenNewPasteKept(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(41_000)+" ")
	require.Empty(t, r.held)

	var cmd tea.Cmd
	for _, k := range r.paste("hello small world") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE hello small world", m.Value())
}

// A letter and a space typed fast after such a paste are both kept.
func TestInputOverflowFastLetterSpaceKept(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(41_000)+" ")
	var cmd tea.Cmd
	for _, k := range r.read("a") {
		m, cmd = m.Update(k)
	}
	move(20 * time.Millisecond)
	for _, k := range r.read(" ") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE a ", m.Value())
}

// The held word followed in the same read by PgUp is dropped, and PgUp
// scrolls the chat as usual (the app, not the input, takes it).
func TestAppOverflowHeldTailThenPgUp(t *testing.T) {
	move := stoppedKeyClock(t)
	m := newAuditApp(t, &fakeToolLLMClient{}, 80, 24)
	m.input = m.input.SetContextWindow(8192)
	for _, r := range "BASE " {
		m, _ = step(t, m, runeKeyOrSpace(r))
	}
	move(time.Second)
	r := &ttyReader{}
	for _, k := range r.paste(rowPaste(40_960)) {
		m, _ = step(t, m, k)
	}
	require.NotEmpty(t, r.held)
	move(2 * time.Second)
	for _, k := range append(r.read(""), tea.KeyMsg{Type: tea.KeyPgUp}) {
		m, _ = step(t, m, k)
	}
	assert.Nil(t, m.input.tailPending)
	assert.Equal(t, "BASE ", m.input.Value())
}

// Review: a key typed right after the glued one (a separate read, inside
// the burst window) is typing too; the held word still never lands.
func TestInputOverflowHeldTailFastTyping(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(40_960))
	require.NotEmpty(t, r.held)
	for _, k := range r.read("x") {
		m, _ = m.Update(k)
	}
	for _, s := range []string{"y", " ", "z"} {
		move(20 * time.Millisecond)
		for _, k := range r.read(s) {
			m, _ = m.Update(k)
		}
	}
	assert.Equal(t, "BASE xy z", m.Value())
}

// K3: when the key that overflows the limit is the space right before
// the word the reader holds back, no dropped key follows the overflow;
// the held word still never lands, and the key typed after it does.
func TestInputOverflowOnLastKeyHeldTailDropped(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetContextWindow(5000)
	limit := m.CharLimit()
	require.Equal(t, 20_000, limit)
	m = typeInput(m, "BASE ")
	move(time.Second)

	// The filler fills the limit exactly; the space after it overflows,
	// and the word after that is held back by the reader.
	filler := rowPaste(limit - len("BASE "))
	word := strings.Repeat("z", 79*ttyReadSize-len(filler)-1)
	paste := filler + " " + word
	require.Zero(t, len(paste)%ttyReadSize)
	require.Less(t, len(word), ttyReadSize)

	r := &ttyReader{}
	for _, k := range r.paste(paste) {
		m, _ = m.Update(k)
	}
	require.Equal(t, word, r.held, "the burst must end on a full read inside the word")
	require.Equal(t, "BASE ", m.Value(), "the paste is rolled back")
	require.Contains(t, m.Notice(), "not inserted")
	move(2 * time.Second)

	var cmd tea.Cmd
	for _, k := range r.read("x") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE x", m.Value())
}

// K3: a one-letter last word ("… and I"), held and then followed in the
// same read by a typed space, is dropped like any other held word; only
// the space lands.
func TestInputOverflowHeldOneLetterTailThenSpace(t *testing.T) {
	move := stoppedKeyClock(t)
	m, r := tailInput(t, move, rowPaste(40_958)+" I")
	require.Equal(t, "I", r.held)

	var cmd tea.Cmd
	for _, k := range r.read(" ") {
		m, cmd = m.Update(k)
	}
	m = settleTicks(t, m, cmd)
	assert.Equal(t, "BASE  ", m.Value())
}
