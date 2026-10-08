// Package termsafe is the one rule for showing untrusted text on a
// terminal: model output, tool results, workspace files and names,
// provider metadata and hook output. A terminal acts on control bytes
// (ESC sequences can move the cursor, erase or rewrite lines, set the
// window title or write the clipboard), and bidi controls can reorder what
// a person reads, so text from outside celeste never reaches the screen
// with those runes live.
//
// Sanitize untrusted text before styling it: celeste's own lipgloss and
// glamour styling is added afterwards and is never touched.
package termsafe

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UnsafeRune returns the first rune of s that could hide or forge text on
// a terminal (C0/C1 controls, DEL, bidi embeddings, overrides and
// isolates, other format runes, line and paragraph separators), or -1.
func UnsafeRune(s string) rune {
	for _, r := range s {
		if lineUnsafe(r) {
			return r
		}
	}
	return -1
}

func lineUnsafe(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || bidi(r) ||
		unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

// bidi reports the runes that reorder the text around them.
func bidi(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) ||
		r == 0x200e || r == 0x200f || r == 0x061c
}

// Line returns s, or s Go-quoted when showing it could act on or hide text
// on a terminal: invalid UTF-8 (it decodes as a printable U+FFFD), a rune
// UnsafeRune rejects (newlines and tabs included), or any other
// non-printable rune. It is for one-line fields a person must read exactly:
// an approval prompt's command, a server or model name, an error.
func Line(s string) string {
	if !utf8.ValidString(s) || UnsafeRune(s) >= 0 || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return strconv.Quote(s)
	}
	return s
}

// Text returns multi-line text with every control made visible: newlines
// and tabs stay, CRLF becomes a newline, and any other C0/C1 control,
// DEL, ESC, bidi control, line or paragraph separator or invalid byte is
// written as its Go escape (\x1b, \r, \u202e, \xff). Everything else,
// emoji joiners included, is kept, so ordinary text reads unchanged.
func Text(s string) string {
	return sanitize(s, false)
}

// Styled is Text for text celeste composed itself that may carry its own
// colors around untrusted parts: a well-formed SGR sequence (ESC [ digits
// and separators m: color and weight; never conceal) is kept, every other ESC
// sequence and control is escaped as Text escapes it.
func Styled(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, keepSGR bool) string {
	if clean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			b.WriteString(`\x`)
			b.WriteString(strconv.FormatUint(uint64(s[i])>>4, 16))
			b.WriteString(strconv.FormatUint(uint64(s[i])&0xf, 16))
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r' && i+1 < len(s) && s[i+1] == '\n':
			// CRLF: the newline that follows is written next.
		case r == 0x1b && keepSGR:
			if n := sgrLen(s[i:]); n > 0 {
				b.WriteString(s[i : i+n])
				i += n
				continue
			}
			b.WriteString(`\x1b`)
		case textUnsafe(r):
			q := strconv.QuoteRuneToASCII(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// clean reports whether s needs no change: valid UTF-8 with no rune Text
// escapes.
func clean(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r != '\n' && r != '\t' && textUnsafe(r) {
			return false
		}
	}
	return true
}

func textUnsafe(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || bidi(r) || r == 0x2028 || r == 0x2029
}

// sgrLen returns the length of the SGR sequence s starts with
// (ESC [ [0-9;:]* m), or 0. A sequence that conceals text (parameter 8)
// is not kept: it could hide what follows.
func sgrLen(s string) int {
	if len(s) < 3 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s) && i < 64; i++ {
		switch c := s[i]; {
		case c == 'm':
			if conceals(s[2:i]) {
				return 0
			}
			return i + 1
		case c >= '0' && c <= '9', c == ';', c == ':':
		default:
			return 0
		}
	}
	return 0
}

// conceals reports whether SGR parameters params turn on conceal (8). The
// operands of an extended color (38, 48, 58: 5;n or 2;r;g;b) are skipped,
// so a color component of 8 is not mistaken for it.
func conceals(params string) bool {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		p := strings.TrimLeft(ps[i], "0")
		if strings.Contains(ps[i], ":") {
			continue // colon form keeps its operands inside one parameter
		}
		switch p {
		case "8":
			return true
		case "38", "48", "58":
			if i+1 < len(ps) {
				switch ps[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return false
}
