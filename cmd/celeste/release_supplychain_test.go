package main

import (
	"fmt"
	"regexp"
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
