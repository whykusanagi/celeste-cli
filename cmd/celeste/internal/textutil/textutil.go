// Package textutil holds the byte-bounded text cut shared by everything
// that caps text on its way to a model or a user, so no cap can split a
// UTF-8 character.
package textutil

import "unicode/utf8"

// CutBytes returns the longest prefix of s that is at most n bytes and does
// not split a UTF-8 sequence. s comes back unchanged when it fits; n <= 0
// gives "".
func CutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
