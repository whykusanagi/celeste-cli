package shellparse

import (
	"strings"
	"testing"
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
	// Count the work, not the time, so a slow or shared CI runner can't fail
	// it: eight times the input is about 8x the steps when linear and 64x
	// when quadratic.
	small, big := strings.Repeat("rm -r x ", 625), strings.Repeat("rm -r x ", 5000)
	if r := workGrowth(func() { DestructiveRm(small) }, func() { DestructiveRm(big) }); r > 12 {
		t.Errorf("8x the rm words did %.1fx the work; want linear (~8x, quadratic is ~64x)", r)
	}
	if DestructiveRm(big) != None {
		t.Fatal("benign line flagged")
	}
	if DestructiveRm(big+"/") != Found {
		t.Error("a trailing / after many rm -r words must be found")
	}
}

// workGrowth is how many times the work of small that big does, in Steps;
// the counter is package-wide, so no test here may run in parallel.
func workGrowth(small, big func()) float64 {
	work := func(f func()) int64 {
		before := Steps()
		f()
		return max(Steps()-before, 1)
	}
	return float64(work(big)) / float64(work(small))
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

// Aikido 806869510: a home-relative path that climbs to or above the home
// directory names it (or something outside it), however it is spelled;
// one that stays below it does not.
func TestDestructiveRmHomeTraversal(t *testing.T) {
	for cmd, want := range map[string]Result{
		"rm -rf ~/../someuser":        Found,
		"rm -rf $HOME/../someuser":    Found,
		"rm -rf ${HOME}/../someuser":  Found,
		"rm -rf ~/../../tmp/x":        Found,
		"rm -rf ~/a/../..":            Found,
		"rm -rf ~/a/..":               Found,
		"rm -rf ~/./../x/*":           Found,
		"rm -rf ~other/../x":          Found,
		"rm -rf ~/project/build":      None,
		"rm -rf ~/a/../b":             None,
		"rm -rf $HOME/project/../tmp": None,
	} {
		if got := DestructiveRm(cmd); got != want {
			t.Errorf("DestructiveRm(%q) = %v, want %v", cmd, got, want)
		}
	}
}
