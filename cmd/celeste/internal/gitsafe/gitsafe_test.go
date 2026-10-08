package gitsafe

import (
	"os"
	"slices"
	"testing"
)

func TestArgsTurnOffRepositoryPrograms(t *testing.T) {
	got := Args("status", "--short")
	want := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "status", "--short"}
	if !slices.Equal(got, want) {
		t.Fatalf("Args = %v, want %v", got, want)
	}
}
