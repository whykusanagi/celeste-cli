package ctxmgr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Under a cap too small for any marker, the result is still at most the
// cap: a plain cut, on a character boundary.
func TestCapsSmallerThanTheMarker(t *testing.T) {
	result := strings.Repeat("é", 500)
	for _, max := range []int{1, 2, 5, 16, 31} {
		got := SnipToolResult(result, max, "")
		if len(got) > max || !utf8.ValidString(got) {
			t.Errorf("SnipToolResult cap %d: %d bytes %q", max, len(got), got)
		}
		capped, wasCapped, err := CapToolResult(result, max, "s", "c", t.TempDir())
		if err != nil || !wasCapped || len(capped) > max || !utf8.ValidString(capped) {
			t.Errorf("CapToolResult cap %d: %d bytes %q (capped %v, err %v)", max, len(capped), capped, wasCapped, err)
		}
	}
}
