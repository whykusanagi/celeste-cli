package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoDoc reads a file at the repository root (two levels up from here).
func repoDoc(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireAll(t *testing.T, name, doc string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(doc, want) {
			t.Errorf("%s lacks %q", name, want)
		}
	}
}

func requireNone(t *testing.T, name, doc string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(doc, bad) {
			t.Errorf("%s still says %q", name, bad)
		}
	}
}

// The README's go install line follows go.mod's module path, so the /v2
// move can't leave it installing 1.x; it explains the self-upgrade, its
// opt-out, the public persona of a source build and persona verify (W5
// Task 15, W7 Task 6).
func TestReadmeInstallAndPersona(t *testing.T) {
	var module string
	for _, line := range strings.Split(repoDoc(t, "go.mod"), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			module = strings.TrimSpace(rest)
		}
	}
	readme := repoDoc(t, "README.md")
	requireAll(t, "README.md", readme,
		"go install "+module+"/cmd/celeste@latest",
		"CELESTE_NO_AUTO_UPGRADE=1",
		"celeste update --check",
		"### Build from source",
		"public persona",
		"celeste persona verify",
		"(VERIFY.md)",
		"docs/HOOKS.md",
		// Spec-coverage audit G6: the upgrade guide and the 2.0 docs are linked.
		"(MIGRATING-2.0.md)",
		"docs/SANDBOX.md", "docs/SUBAGENTS.md", "docs/PLAN_MODE.md",
		"docs/STEERING.md", "docs/PERSONALITY.md", "docs/ROADMAP.md",
		"docs/COMPARISON.md", "docs/ACP.md",
		"celeste revert", "celeste hooks trust",
	)
	requireNone(t, "README.md", readme, "celeste_essence.json", "hooks defined in `.grimoire`")
}

// docs/PERSONALITY.md describes how the persona is built, chosen and
// verified, not the character (W5 ruling 22).
func TestPersonalityDocDescribesTheMechanism(t *testing.T) {
	doc := repoDoc(t, "docs/PERSONALITY.md")
	requireAll(t, "docs/PERSONALITY.md", doc,
		"celeste persona verify", "public persona", "context_limit",
		"`full`", "`spine`", "`lite`", "`off`", "CELESTE_NO_AUTO_UPGRADE=1",
		"cmd/celeste/prompts/persona/LICENSE",
	)
	requireNone(t, "docs/PERSONALITY.md", doc, "Lewd, chaotic", "<300 words", "Full system prompt for LLM")
}

// VERIFY.md tells a go install user how to check the self-upgraded binary,
// and what the updater checks.
func TestVerifyDocCoversGoInstall(t *testing.T) {
	doc := repoDoc(t, "VERIFY.md")
	requireAll(t, "VERIFY.md", doc,
		"celeste persona verify", "CELESTE_NO_AUTO_UPGRADE=1",
		"## What `go install` and `celeste update` check",
		// The archives hold celeste-<os>-<arch> (selfupdate.Platforms).
		"`celeste-<os>-<arch>`", "`celeste-windows-amd64.exe`",
		"xattr -dr com.apple.quarantine ./celeste-darwin-arm64",
		"mv ./celeste-darwin-arm64 ~/.local/bin/celeste",
	)
	requireNone(t, "VERIFY.md", doc,
		"mv ./celeste ", "unpacks the `celeste` binary", "`./celeste persona verify`",
		"quarantine ./celeste\n", "(`celeste`, or `celeste.exe`",
	)
}

// The persona guard keeps lite while it fits in half the window
// (prompts/guard.go), and the Plans table ends before the next heading.
func TestMigratingPersonaGuardAndTables(t *testing.T) {
	doc := repoDoc(t, "MIGRATING-2.0.md")
	requireAll(t, "MIGRATING-2.0.md", doc, "`lite` stays while it fits in half the window")
	if regexp.MustCompile(`\|\n#`).MatchString(doc) {
		t.Error("MIGRATING-2.0.md: a table runs straight into a heading; add a blank line")
	}
}

// /plan enters plan mode and approved plans live in .celeste/plan.json
// (#292); the routing diagram must not send it to plan.md.
func TestRoutingDocPlanMode(t *testing.T) {
	doc := repoDoc(t, "docs/ROUTING.md")
	requireNone(t, "docs/ROUTING.md", doc, "plan.md")
	requireAll(t, "docs/ROUTING.md", doc, ".celeste/plan.json")
}

// The release page points users at VERIFY.md and persona verify.
func TestReleaseNotesLinkVerify(t *testing.T) {
	doc := repoDoc(t, ".github/workflows/release.yml")
	requireAll(t, "release.yml", doc, "VERIFY.md", "celeste persona verify")
}

// `celeste version` (like help, update and persona) never runs the startup
// self-upgrade (autoupgrade.go noUpgradeCommands), so no doc may tell a go
// install user that it upgrades the binary; `celeste update` does.
func TestDocsUpgradeAGoInstallBuildWithUpdate(t *testing.T) {
	for _, name := range []string{"README.md", "VERIFY.md", "MIGRATING-2.0.md", "docs/PERSONALITY.md", "RELEASE_CHECKLIST.md"} {
		doc := repoDoc(t, name)
		requireNone(t, name, doc, "run `celeste version` first", "celeste version` first", "upgrades itself on `celeste version`", "this also upgrades it")
		requireAll(t, name, doc, "celeste update")
	}
	for _, cmd := range []string{"help", "version", "update", "persona"} {
		if !noUpgradeCommands[cmd] {
			t.Fatalf("%s now runs the startup self-upgrade; revisit the docs this test pins", cmd)
		}
	}
}

// The release checklist points at the W7 go/no-go and post-release checks
// instead of copying them.
func TestReleaseChecklistReferencesTheW7Checks(t *testing.T) {
	doc := repoDoc(t, "RELEASE_CHECKLIST.md")
	requireAll(t, "RELEASE_CHECKLIST.md", doc, "Go / no-go", "Post-release checks", "celeste persona verify")
}

// No current doc describes Celeste's character or speaks in her voice (W5
// ruling 22). The regex is a floor: the changelog, plans, the slider
// authoring notes and the built-in stream rules (which match her voice on
// purpose) are out of scope.
func TestDocsDescribeThePersonaMechanismOnly(t *testing.T) {
	rx := regexp.MustCompile(`succubus|oni-|[Oo]nii|\btwin\b|\bKusanagi\b|Operational Laws|\bLaws? [0-5]\b|pauldron|fraternal|Abyssal Intelligence|[Dd]emon [Nn]oble|VTuber|lore-accurate|Hehe|cutie~|I.m Celeste|smug wrath|The Abyss whispers|face my wrath|cuties~|I.ll possess|I.ll bully|watch me own|Summoning Me|\bDarlings\b|\bmy \d+ tools`)
	root := filepath.Join("..", "..")
	skip := map[string]bool{
		"CHANGELOG.md":                 true,
		"docs/superpowers":             true,
		"docs/slider-agent-handoff.md": true,
		"cmd/celeste/rules/builtin":    true,
		"node_modules":                 true,
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || skip[rel] || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if skip[rel] || !strings.HasSuffix(rel, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if rx.MatchString(line) {
				t.Errorf("%s:%d describes the character: %.80s", rel, i+1, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// #310 shipped in 2.0: the docs describe the tool fit and the reported
// local window, and no longer defer the fix to 2.1.
func TestDocsDescribeTheSmallWindowToolFit(t *testing.T) {
	mig := repoDoc(t, "MIGRATING-2.0.md")
	requireAll(t, "MIGRATING-2.0.md", mig, "core set of tools", "find_tools")
	prov := repoDoc(t, "docs/LLM_PROVIDERS.md")
	requireAll(t, "docs/LLM_PROVIDERS.md", prov, "/api/ps", "/props", "reported by the server", "core set of tools")
	for _, name := range []string{"MIGRATING-2.0.md", "docs/ROADMAP.md", "CHANGELOG.md"} {
		requireNone(t, name, repoDoc(t, name), "planned for 2.1", "stays for 2.1")
	}
}
