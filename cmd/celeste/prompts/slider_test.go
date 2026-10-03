package prompts

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// The warmth slider's extremes must read as different instructions, and
// moving warmth must change only the warmth line of the slider block (the
// W5 back-test found warmth 0 and 10 inaudible; the anchors were rewritten).
func TestWarmthExtremesChangeOnlyTheWarmthLine(t *testing.T) {
	block := func(w int) []string {
		s := config.DefaultSliderConfig()
		s.Warmth = w
		return strings.Split(ComposeSliderPrompt(s), "\n")
	}
	cold, warm := block(0), block(10)
	if len(cold) != len(warm) {
		t.Fatalf("slider blocks differ in shape: %d vs %d lines", len(cold), len(warm))
	}
	changed := 0
	for i := range cold {
		if cold[i] == warm[i] {
			continue
		}
		changed++
		if !strings.HasPrefix(cold[i], "Warmth Level: ") || !strings.HasPrefix(warm[i], "Warmth Level: ") {
			t.Errorf("warmth changed a non-warmth line:\n%q\n%q", cold[i], warm[i])
		}
	}
	if changed != 1 {
		t.Errorf("warmth 0 vs 10 changed %d lines, want 1", changed)
	}
}

// Every anchor is non-empty and ends a sentence, so a composed block never
// carries a dangling fragment.
func TestSliderAnchorsAreComplete(t *testing.T) {
	sets := map[string][4]string{"flirt": flirtAnchors, "warmth": warmthAnchors, "register": registerAnchors, "lewdness": lewdnessAnchors}
	for name, set := range sets {
		for i, a := range set {
			if a = strings.TrimSpace(a); a == "" || !strings.HasSuffix(a, ".") {
				t.Errorf("%s anchor %d is empty or unterminated", name, i)
			}
		}
	}
}
