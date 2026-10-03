package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review Focus 4 / ruling 12: celeste plan shows the approved plan with
// each step's todo status, a legacy plan.md with a note, or "No plan yet."
func TestPlanCommandShowsTodoStatus(t *testing.T) {
	ws := t.TempDir()
	var out, errOut bytes.Buffer
	if code := planCLI(nil, ws, &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != "No plan yet." {
		t.Fatalf("empty: code %d out %q err %q", code, out.String(), errOut.String())
	}

	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".celeste", "plan.md"), []byte("- [ ] old step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := planCLI([]string{"show"}, ws, &out, &errOut); code != 0 {
		t.Fatalf("legacy: code %d", code)
	}
	if !strings.Contains(out.String(), "- [ ] old step") || !strings.Contains(out.String(), "plan.json") {
		t.Fatalf("legacy output = %q", out.String())
	}

	writePlanFixture(t, ws)
	out.Reset()
	if code := planCLI(nil, ws, &out, &errOut); code != 0 {
		t.Fatalf("plan: code %d", code)
	}
	got := out.String()
	for _, want := range []string{"[x] 1. write tests", "[ ] 2. implement"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old step") {
		t.Fatal("plan.json wins over a legacy plan.md")
	}

	out.Reset()
	errOut.Reset()
	if code := planCLI([]string{"bogus"}, ws, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "Usage: celeste plan") {
		t.Fatalf("bad args: code %d err %q", code, errOut.String())
	}
}
