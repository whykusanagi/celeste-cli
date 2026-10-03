package loop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
)

func ruleNames(env *Env) map[string]bool {
	names := map[string]bool{}
	for _, r := range env.Rules.Rules {
		names[r.Name] = true
	}
	return names
}

// Setup loads the stream rules for every mode (2.0 W3): built-ins, the
// user's ~/.celeste/rules, then the workspace grimoire's "## Stream Rules";
// a bad rule is a warning, and a nested Env in the same workspace shares
// the parent's set. A repo grimoire's rules load only once trusted
// (ruling 18): a non-interactive run skips them with a warning.
func TestSetupLoadsStreamRules(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(home, ".celeste", "rules", "mine.md"), "---\ncondition: foo\n---\nNo foo.\n")
	write(t, filepath.Join(home, ".celeste", "rules", "bad.md"), "no frontmatter")
	grim := filepath.Join(ws, ".grimoire")
	write(t, grim, "## Stream Rules\n### repo-rule\n---\ncondition: bar\n---\nNo bar.\n")

	env, w := mustSetup(t, ModeAgent, ws)
	names := ruleNames(env)
	for _, want := range []string{"mine", "unbacked-audio-claim"} {
		if !names[want] {
			t.Errorf("rule %s not loaded: %v", want, names)
		}
	}
	if names["repo-rule"] {
		t.Error("an untrusted repo rule loaded in a non-interactive run")
	}
	if !strings.Contains(w.all(), "bad.md") || !strings.Contains(w.all(), "celeste hooks trust") {
		t.Errorf("a bad rule and an untrusted section must be warnings:\n%s", w.all())
	}

	srcs, _, err := hooks.Discover(ws, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if s.Kind == hooks.KindRepoStreamRules {
			if err := hooks.LoadTrust(home).Approve(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	env, _ = mustSetup(t, ModeAgent, ws)
	if !ruleNames(env)["repo-rule"] {
		t.Fatalf("a trusted repo rule must load: %v", ruleNames(env))
	}
	if strings.Contains(env.ProjectContext, "No bar.") {
		t.Error("a stream rule must cost no context until it fires")
	}
	child := mustNested(t, env, NestedOptions{})
	if child.Rules != env.Rules {
		t.Error("a nested Env in the same workspace shares the parent's rules")
	}
}

// The chat asks once through its approver, as it does for repo hooks.
func TestSetupChatApprovesStreamRules(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".grimoire"), "## Stream Rules\n### repo-rule\n---\ncondition: bar\n---\nNo bar.\n")
	asked := 0
	approve := func(src hooks.Source, _ hooks.TrustStatus) bool {
		if src.Kind == hooks.KindRepoStreamRules {
			asked++
		}
		return true
	}
	env, err := Setup(ModeChat, testCfg(), ws, SetupOptions{Warn: func(string) {}, Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	if asked != 1 || !ruleNames(env)["repo-rule"] {
		t.Fatalf("asked=%d rules=%v", asked, ruleNames(env))
	}
}
