package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The release signing key: primary and the signing subkey that signs tags
// and release artifacts.
const (
	releasePrimaryFpr = "940490EF09DA31322BF7FD83875849AB1D541C55"
	releaseSubkeyFpr  = "F4C254F6EE5D7F086C921DEBA6BB54DDC70EE8FB"
)

// needsAll reports whether a job's needs (a string or a list) names every
// job in want.
func needsAll(needs any, want ...string) bool {
	have := map[string]bool{}
	switch n := needs.(type) {
	case string:
		have[n] = true
	case []any:
		for _, v := range n {
			have[fmt.Sprint(v)] = true
		}
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

// Aikido 806869730: no release job sees a secret until a job that holds none
// has checked that the pushed tag is signed by the release key (taken from
// main, not from the tag) and that the tagged commit is on main.
func TestReleaseWorkflowVerifiesTheTagBeforeAnySecret(t *testing.T) {
	_, wf := readWorkflow(t, "release.yml")
	gate, ok := wf.Jobs["verify-tag"]
	if !ok {
		t.Fatal("release.yml has no verify-tag job")
	}
	if gate.Needs != nil {
		t.Errorf("verify-tag needs %v: it must run first", gate.Needs)
	}
	var script strings.Builder
	for _, s := range gate.Steps {
		script.WriteString(s.Run)
		for k, v := range s.Env {
			if strings.Contains(v, "secrets.") {
				t.Errorf("verify-tag step %q reads a secret through %s", s.Name, k)
			}
		}
		if strings.Contains(s.Run, "secrets.") {
			t.Errorf("verify-tag step %q reads a secret", s.Name)
		}
		if strings.HasPrefix(s.Uses, "actions/checkout") && s.With["persist-credentials"] != false {
			t.Errorf("verify-tag checkout keeps git credentials")
		}
	}
	run := script.String()
	for _, want := range []string{
		`git fetch --force --no-tags origin "+${GITHUB_REF}:${GITHUB_REF}"`,
		"git show origin/main:whykusanagi.asc",
		"git verify-tag --raw",
		"VALIDSIG",
		releasePrimaryFpr,
		releaseSubkeyFpr,
		`git merge-base --is-ancestor "$GITHUB_SHA" origin/main`,
	} {
		if !strings.Contains(run, want) {
			t.Errorf("verify-tag does not run %q", want)
		}
	}
	// The key never comes from the tag's own tree.
	if regexp.MustCompile(`gpg[^\n]*--import[^\n]*whykusanagi\.asc`).MatchString(run) {
		t.Error("verify-tag imports whykusanagi.asc from the checked-out tag")
	}
	// Dry runs without a tag skip the check; a tag never does.
	if !strings.Contains(run, `"$GITHUB_EVENT_NAME" != push`) || !strings.Contains(run, "refs/tags/*") {
		t.Error("verify-tag does not limit its skip to non-tag workflow_dispatch runs")
	}
	if !needsAll(wf.Jobs["build"].Needs, "verify-tag") {
		t.Errorf("build needs %v, want verify-tag", wf.Jobs["build"].Needs)
	}
	if !needsAll(wf.Jobs["release"].Needs, "verify-tag", "build") {
		t.Errorf("release needs %v, want verify-tag and build", wf.Jobs["release"].Needs)
	}
	// Every secret is read after the gate: only in build and release.
	for name, job := range wf.Jobs {
		if name == "build" || name == "release" {
			continue
		}
		for _, s := range job.Steps {
			if strings.Contains(s.Run, "secrets.") || strings.Contains(fmt.Sprint(s.Env), "secrets.") || strings.Contains(fmt.Sprint(s.With), "secrets.") {
				t.Errorf("job %s step %q reads a secret", name, s.Name)
			}
		}
	}
}

// Aikido 806869730 hardening: the workflow grants nothing by default and
// only the release job can write contents.
func TestReleaseWorkflowLeastPrivilege(t *testing.T) {
	_, wf := readWorkflow(t, "release.yml")
	if m, ok := wf.Permissions.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("release.yml top-level permissions = %v, want {}", wf.Permissions)
	}
	for name, job := range wf.Jobs {
		perms, _ := job.Permissions.(map[string]any)
		for scope, level := range perms {
			if level == "write" && !(name == "release" && scope == "contents") {
				t.Errorf("job %s has %s: write", name, scope)
			}
		}
	}
	if p, _ := wf.Jobs["release"].Permissions.(map[string]any); p["contents"] != "write" {
		t.Errorf("release job permissions = %v, want contents: write", wf.Jobs["release"].Permissions)
	}
	// Aikido 806780680: the release job publishes through the API and
	// never pushes, so its checkout keeps no token in .git/config.
	for name, job := range wf.Jobs {
		for _, s := range job.Steps {
			if strings.HasPrefix(s.Uses, "actions/checkout") && s.With["persist-credentials"] != false {
				t.Errorf("release.yml job %s: checkout persists credentials", name)
			}
		}
	}
}

// verify-tag requires main's key file to hold exactly the release primary,
// as `make import-key` does, not merely to mention it somewhere.
func TestReleaseWorkflowKeyFileHoldsExactlyThePrimary(t *testing.T) {
	_, wf := readWorkflow(t, "release.yml")
	var run strings.Builder
	for _, s := range wf.Jobs["verify-tag"].Steps {
		run.WriteString(s.Run)
	}
	script := run.String()
	if !strings.Contains(script, `$1 == "pub" {p = 1; next} p && $1 == "fpr" {print $10; p = 0}`) {
		t.Error("verify-tag does not read the primary fingerprint of each key in main's key file")
	}
	if !strings.Contains(script, `[ "$primaries" != "$PRIMARY" ]`) {
		t.Error("verify-tag does not require main's key file to hold exactly the release primary")
	}
	if strings.Contains(script, `*"$PRIMARY"*`) {
		t.Error("verify-tag still accepts a key file that merely mentions the primary")
	}
}

// Aikido review on #423: a v* ref can point at a signed tag object made for
// another tag name. verify-tag reads the name the tag object was signed
// for (its own header, never its message) and requires it to be the ref's.
func TestReleaseWorkflowBindsTheSignedTagNameToTheRef(t *testing.T) {
	_, wf := readWorkflow(t, "release.yml")
	var run strings.Builder
	for _, s := range wf.Jobs["verify-tag"].Steps {
		run.WriteString(s.Run)
	}
	script := run.String()
	if !strings.Contains(script, `[ "$signed_tag" != "$GITHUB_REF_NAME" ]`) {
		t.Fatal("verify-tag does not compare the tag object's own name with GITHUB_REF_NAME")
	}
	line := regexp.MustCompile(`(?m)^\s*(signed_tag="\$\(.*\)")\s*$`).FindStringSubmatch(script)
	if line == nil {
		t.Fatal("verify-tag does not read signed_tag from the tag object")
	}
	if runtime.GOOS == "windows" {
		t.Skip("runs the extracted shell line with bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	env := []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "gitconfig"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid"}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "c")
	// The old tag's message itself mentions another name on a "tag " line.
	git("tag", "-a", "v1.0.0", "-m", "release\n\ntag v2.0.0")
	obj := git("rev-parse", "refs/tags/v1.0.0")
	git("update-ref", "refs/tags/v2.0.0", obj)
	for ref, want := range map[string]string{"refs/tags/v2.0.0": "v1.0.0", "refs/tags/v1.0.0": "v1.0.0"} {
		cmd := exec.Command(bash, "-c", "set -euo pipefail; "+line[1]+`; printf '%s' "$signed_tag"`)
		cmd.Dir = dir
		cmd.Env = append(env, "GITHUB_REF="+ref)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if string(out) != want {
			t.Errorf("signed_tag for %s = %q, want %q", ref, out, want)
		}
	}
}
