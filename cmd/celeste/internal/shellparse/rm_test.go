package shellparse

import (
	"strings"
	"testing"
	"time"
)

// A later rm word does not end an earlier rm's arguments: rm -r rm /
// removes / (and a file named rm).
func TestDestructiveRmArgsRunPastAnotherRm(t *testing.T) {
	for cmd, want := range map[string]Result{
		"rm -r rm /":          Found,
		"rm -f rm -r /usr":    Found,
		"rm rm -r ./build":    None,
		"xargs rm -r -f /usr": Found,
		"rm -r -- --rm x":     None,
	} {
		if got := DestructiveRm(cmd); got != want {
			t.Errorf("DestructiveRm(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// Every word that is rm starts a check; the checks share one pass, so a
// long line of rm words stays linear (review of cleanup-4: 40 KB took 2 s).
func TestDestructiveRmLinearOnManyRmWords(t *testing.T) {
	// Compare growth, not wall time, so a slow CI runner can't fail it: four
	// times the input costs about 4x when linear and 16x when quadratic.
	small, big := strings.Repeat("rm -r x ", 1250), strings.Repeat("rm -r x ", 5000)
	if r := growth(func() { DestructiveRm(small) }, func() { DestructiveRm(big) }); r > 8 {
		t.Errorf("4x the rm words took %.1fx as long; want linear (~4x, quadratic is ~16x)", r)
	}
	if DestructiveRm(big) != None {
		t.Fatal("benign line flagged")
	}
	if DestructiveRm(big+"/") != Found {
		t.Error("a trailing / after many rm -r words must be found")
	}
}

// growth is how many times longer big takes than small, each timed as the
// best of five runs.
func growth(small, big func()) float64 {
	best := func(f func()) time.Duration {
		d := time.Duration(1<<63 - 1)
		for i := 0; i < 5; i++ {
			start := time.Now()
			f()
			d = min(d, time.Since(start))
		}
		return max(d, time.Microsecond)
	}
	return float64(best(big)) / float64(best(small))
}

func BenchmarkDestructiveRmManyRmWords(b *testing.B) {
	cmd := strings.Repeat("rm -r x ", 5000)
	for i := 0; i < b.N; i++ {
		DestructiveRm(cmd)
	}
}

// The one-pass reading agrees with reading each rm's arguments on its own
// (RmFlags), the obvious quadratic form.
func FuzzRmRefusedMatchesRmFlags(f *testing.F) {
	for _, s := range []string{"rm -r rm /", "rm -rf -- > / x", "rm > -r / -f", "xargs rm --rec ~ rm -f /a/b", "rm -r -- -- /"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		words := strings.Fields(s)
		want := false
		for i, w := range words {
			if CommandName(w) != "rm" {
				continue
			}
			recursive, force, targets := RmFlags(words[i+1:])
			for _, tg := range targets {
				if recursive && (criticalPath(tg) || force && systemOrHomePath(tg)) {
					want = true
				}
			}
		}
		if got := RmRefused(words); got != want {
			t.Errorf("RmRefused(%q) = %v, want %v", words, got, want)
		}
	})
}
