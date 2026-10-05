package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The held tail of an overflowed unbracketed paste (K3).
//
// Bubble Tea v1's reader parses each read of at most 256 bytes. When a
// read fills its buffer and ends inside a run of printable characters, it
// holds that run back in case the rest is still to come (key.go,
// canHaveMoreData), and parses it with the next read. When the paste ends
// exactly there, the next read is whatever the user does next, seconds
// later: the paste's last word arrives then, glued to a typed letter
// ("0123" + "x" is one message, "0123x"), or as its own message right
// before a space, Enter or another key from the same read. That is long
// after the overflow's drop window (pasteRestoreGap), so on its own it
// would land in the rolled-back input as typing.
//
// Such a run can only follow a burst whose last delivered key was not a
// rune run: the held run is a whole run, cut off by the space or newline
// before it. So after such a burst overflowed, the first rune run is held
// for a burst window and settled by what comes with it, from the same
// read or the same burst:
//
//   - nothing: the paste's last word and the key typed after it, glued
//     into one message; only the typed key, its last rune, is inserted
//     (a single rune is all typed);
//   - another rune run (after any spaces): a new paste, or fast typing;
//     everything held is inserted;
//   - spaces, then nothing: the paste's last word and a typed space; the
//     space is inserted. A single held rune is kept with it: someone
//     typing one letter and a space fast;
//   - any other key (Enter, an arrow, Esc): the paste's last word alone;
//     dropped, and the key acts as usual.
//
// The reader's messages carry no read boundaries, so two cases stay
// ambiguous and are settled for the likelier one: a held word glued to the
// first word of a second unbracketed paste is kept (it reads as that
// paste), and several characters committed at once as the very first
// input after such an overflow (an input method) keep only their last.

// tailSettleMsg settles held keys once a burst window has passed with no
// other key (the tick for tailSeq seq).
type tailSettleMsg struct{ seq int }

// plainRuneRun reports whether k is a run of printable characters as the
// terminal reader delivers it: not a bracketed paste, not Alt.
func plainRuneRun(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && !k.Paste && !k.Alt && len(k.Runes) > 0
}

// holdTail holds runes, which arrived at now, until the burst window
// settles them, and returns the tick that settles them if no key does.
func (m *InputModel) holdTail(runes []rune, now time.Time) tea.Cmd {
	m.tailPending = append([]rune(nil), runes...)
	m.tailSpaces = 0
	return m.tailTick(now)
}

// tailTick restarts the settle window at now.
func (m *InputModel) tailTick(now time.Time) tea.Cmd {
	m.tailAt = now
	m.tailSeq++
	seq := m.tailSeq
	return tea.Tick(burstWindow, func(time.Time) tea.Msg { return tailSettleMsg{seq: seq} })
}

// SettleTail settles held keys by k, the next key, when k does not join
// them (see the top of this file). The app calls it for every key before
// routing it, so a key another view takes (Esc, Ctrl+C, PgUp) settles them
// too. A space the input would hold is left for the input's Update.
func (m InputModel) SettleTail(k tea.KeyMsg) InputModel {
	if m.tailPending == nil || (k.Type == tea.KeySpace && keyClock().Sub(m.tailAt) < burstWindow) {
		return m
	}
	m, _, _ = m.settleTail(k)
	return m
}

// settleTail settles held keys by k. held reports that k was held with
// them (a space inside the window), with the tick that settles them.
func (m InputModel) settleTail(k tea.KeyMsg) (_ InputModel, held bool, cmd tea.Cmd) {
	if m.tailPending == nil {
		return m, false, nil
	}
	now := keyClock()
	switch {
	case now.Sub(m.tailAt) >= burstWindow:
		m.settleTailQuiet()
	case k.Type == tea.KeySpace:
		m.tailSpaces++
		return m, true, m.tailTick(now)
	case plainRuneRun(k):
		m.insertSettled(m.tailPending, m.tailSpaces)
	default:
		m.insertSettled(nil, m.tailSpaces)
	}
	m.tailPending, m.tailSpaces = nil, 0
	return m, false, nil
}

// settleTailQuiet settles held keys after a burst window with no other
// key.
func (m *InputModel) settleTailQuiet() {
	runes, spaces := m.tailPending, m.tailSpaces
	if runes == nil {
		return
	}
	m.tailPending, m.tailSpaces = nil, 0
	switch {
	case spaces == 0:
		m.insertSettled(runes[len(runes)-1:], 0)
	case len(runes) == 1:
		m.insertSettled(runes, spaces)
	default:
		m.insertSettled(nil, spaces)
	}
}

// insertSettled inserts runes and then spaces spaces the way keys' text
// is inserted, as the start of a burst: an overflowing paste that follows
// rolls back to here.
func (m *InputModel) insertSettled(runes []rune, spaces int) {
	for range spaces {
		runes = append(runes, ' ')
	}
	if len(runes) == 0 {
		return
	}
	m.markBurstBase()
	m.burstPaste = len(runes) > 1
	if !m.admit(runes, false) {
		return
	}
	m.textArea.InsertString(string(runes))
	m.refreshSuggestions()
}
