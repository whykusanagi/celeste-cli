package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCutBytes(t *testing.T) {
	for _, tc := range []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"", 3, ""},
		{"aセ", 2, "a"}, // セ is 3 bytes: a cut at 2 or 3 backs off to "a"
		{"aセ", 3, "a"},
		{"aセ", 4, "aセ"},
		{"セレステ", 7, "セレ"},
		{"😈x", 3, ""},
	} {
		if got := CutBytes(tc.s, tc.n); got != tc.want {
			t.Errorf("CutBytes(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
		}
	}
}

func TestCutBytesNeverSplitsARune(t *testing.T) {
	s := strings.Repeat("aセ😈é", 50)
	for n := 0; n <= len(s)+1; n++ {
		got := CutBytes(s, n)
		if len(got) > n && n >= 0 || !utf8.ValidString(got) || !strings.HasPrefix(s, got) {
			t.Fatalf("CutBytes(_, %d) = %d bytes, valid=%v", n, len(got), utf8.ValidString(got))
		}
	}
}
