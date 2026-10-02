package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
)

const repoRuleBody = "### repo-rule\n---\ncondition: baz\n---\nNo baz."

func trustHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func approveSection(t *testing.T, home string, s Section) {
	t.Helper()
	if err := hooks.LoadTrust(home).Approve(hooks.StreamRulesSource(s.Source, s.Body)); err != nil {
		t.Fatal(err)
	}
}

// A repo grimoire's "## Stream Rules" goes through the hooks trust store
// (ruling 18, coordinator override): untrusted is skipped with one warning,
// trusted loads, and an edit skips it again until it is re-trusted.
func TestRepoStreamRulesNeedTrust(t *testing.T) {
	home := trustHome(t)
	ws := t.TempDir()
	sec := Section{Source: filepath.Join(ws, ".grimoire"), Body: repoRuleBody}
	other := Section{Source: filepath.Join(ws, ".grimoire.local"), Body: "### local\n---\ncondition: q\n---\nNo q."}
	touch(t, sec.Source, other.Source)

	var warns []string
	warn := func(s string) { warns = append(warns, s) }
	if got := Trusted(home, []Section{sec, other}, nil, warn); len(got) != 0 {
		t.Fatalf("untrusted sections loaded: %+v", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "celeste hooks trust") || !strings.Contains(warns[0], ".grimoire.local") {
		t.Fatalf("want one warning naming both files and the trust command, got %v", warns)
	}

	approveSection(t, home, sec)
	warns = nil
	if got := Trusted(home, []Section{sec, other}, nil, warn); len(got) != 1 || got[0].Source != sec.Source {
		t.Fatalf("trusted section must load alone: %+v", got)
	}
	if len(warns) != 1 || strings.Contains(warns[0], sec.Source+"\"") {
		t.Errorf("only the untrusted file is warned about: %v", warns)
	}

	edited := Section{Source: sec.Source, Body: strings.Replace(repoRuleBody, "baz", "qux", 1)}
	warns = nil
	if got := Trusted(home, []Section{edited}, nil, warn); len(got) != 0 {
		t.Fatalf("an edited section must be skipped until re-trusted: %+v", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "changed") {
		t.Errorf("warnings = %v", warns)
	}
	approveSection(t, home, edited)
	if got := Trusted(home, []Section{edited}, nil, warn); len(got) != 1 {
		t.Fatalf("re-trusted section must load: %+v", got)
	}

	// The rules a trusted section adds reach Load.
	set := Load(home, Trusted(home, []Section{edited}, nil, nil), nil)
	found := false
	for _, r := range set.Rules {
		found = found || r.Name == "repo-rule"
	}
	if !found {
		t.Error("a trusted repo rule must load")
	}
}

// The interactive chat asks once; a yes is persisted, a no is skipped.
func TestRepoStreamRulesApprover(t *testing.T) {
	home := trustHome(t)
	sec := Section{Source: filepath.Join(t.TempDir(), ".grimoire"), Body: repoRuleBody}
	touch(t, sec.Source)
	calls := 0
	no := func(hooks.Source, hooks.TrustStatus) bool { calls++; return false }
	var warns []string
	if got := Trusted(home, []Section{sec}, no, func(s string) { warns = append(warns, s) }); len(got) != 0 || calls != 1 || len(warns) != 1 {
		t.Fatalf("declined: got=%v calls=%d warns=%v", got, calls, warns)
	}
	var asked hooks.Source
	yes := func(src hooks.Source, _ hooks.TrustStatus) bool { calls++; asked = src; return true }
	if got := Trusted(home, []Section{sec}, yes, nil); len(got) != 1 || calls != 2 {
		t.Fatalf("approved: got=%v calls=%d", got, calls)
	}
	if asked.Kind != hooks.KindRepoStreamRules || !strings.Contains(asked.Rules, "condition: baz") {
		t.Errorf("the approver must see the rules: %+v", asked)
	}
	if got := Trusted(home, []Section{sec}, nil, nil); len(got) != 1 || calls != 2 {
		t.Errorf("an approval must persist without asking again: got=%v calls=%d", got, calls)
	}
}

// Built-ins and the user's own rules need no trust: the global grimoire
// (~/.celeste/grimoire.md) is the user's file.
func TestGlobalGrimoireStreamRulesNeedNoTrust(t *testing.T) {
	home := trustHome(t)
	sec := Section{Source: filepath.Join(home, ".celeste", "grimoire.md"), Body: repoRuleBody}
	var warns []string
	if got := Trusted(home, []Section{sec}, nil, func(s string) { warns = append(warns, s) }); len(got) != 1 || len(warns) != 0 {
		t.Errorf("global grimoire: got=%v warns=%v", got, warns)
	}
}

// A symlinked repo grimoire is refused before anyone is asked, exactly as
// `celeste hooks trust` refuses it (review M4).
func TestSymlinkedRepoGrimoireIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs Developer Mode or admin rights on Windows runners")
	}
	home := trustHome(t)
	ws := t.TempDir()
	real := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(real, []byte("## Stream Rules\n"+repoRuleBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, ".grimoire")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	asked := 0
	yes := func(hooks.Source, hooks.TrustStatus) bool { asked++; return true }
	var warns []string
	got := Trusted(home, []Section{{Source: link, Body: repoRuleBody}}, yes, func(s string) { warns = append(warns, s) })
	if len(got) != 0 || asked != 0 {
		t.Fatalf("symlinked grimoire: got=%v asked=%d", got, asked)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "symlinked") {
		t.Errorf("warnings = %v", warns)
	}
}

// touch creates the grimoire files a section names (the trust gate refuses
// a file it cannot check).
func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("## Stream Rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
