package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
)

// A diff base is a revision, never an option: a base that git would read
// as an option is refused and nothing is written in the workspace.
func TestParseGitDiffRangesRefusesOptionBase(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "one")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, base := range []string{"--output=out.txt", "-p", "--ext-diff"} {
		if _, err := ParseGitDiffRanges(dir, base); err == nil {
			t.Errorf("base %q: want an error, got none", base)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err == nil {
		t.Fatal("an option base wrote a file in the workspace")
	}

	// An ordinary revision still works.
	ranges, err := ParseGitDiffRanges(dir, "HEAD")
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	if len(ranges) != 1 || ranges[0].File != "a.txt" {
		t.Fatalf("ranges = %+v", ranges)
	}
}
