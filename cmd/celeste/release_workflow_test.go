package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// W5 ruling 20, audit #8 W1: release builds inject the persona key from the
// CELESTE_PERSONA_KEY secret, run `persona verify` on the linux-amd64 build
// before anything is archived, signed or published, and fail if any
// artifact's build info records the key. Without this every official
// release ships the public persona.
func TestReleaseWorkflowInjectsAndVerifiesThePersonaKey(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(b)
	for _, want := range []string{
		"CELESTE_PERSONA_KEY: ${{ secrets.CELESTE_PERSONA_KEY }}",
		"-X github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts.personaKey=",
		"go build -trimpath",
		"./dist/celeste-linux-amd64 persona verify",
		`info="$(go version -m "$f")"`,
		"*personaKey*)",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("release.yml lacks %q", want)
		}
	}
	// The secret is read from the environment, never interpolated into a
	// run script, and nothing prints the key or the flags that carry it.
	if n := strings.Count(wf, "secrets.CELESTE_PERSONA_KEY"); n != 1 {
		t.Errorf("secrets.CELESTE_PERSONA_KEY appears %d times, want once (the env line)", n)
	}
	for _, bad := range []string{"set -x", `echo "$LDFLAGS"`, "echo $LDFLAGS", `echo "$CELESTE_PERSONA_KEY"`, "echo $CELESTE_PERSONA_KEY", `echo "$KEY"`} {
		if strings.Contains(wf, bad) {
			t.Errorf("release.yml contains %q", bad)
		}
	}
	// Order: build, then verify the persona, then archive.
	build := strings.Index(wf, "- name: Build binaries")
	verify := strings.Index(wf, "- name: Verify the persona")
	archive := strings.Index(wf, "- name: Create archives")
	if build < 0 || verify < 0 || archive < 0 || !(build < verify && verify < archive) {
		t.Errorf("step order: build %d, verify the persona %d, archives %d", build, verify, archive)
	}
}

// Only release.yml may see the key (ruling 15).
func TestOnlyTheReleaseWorkflowReadsThePersonaKey(t *testing.T) {
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "release.yml" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "CELESTE_PERSONA_KEY") {
			t.Errorf("%s references CELESTE_PERSONA_KEY", e.Name())
		}
	}
}

// Ruling 1: release-please opens the release PR but never creates the
// release; the owner pushes a GPG-signed tag, which runs release.yml.
func TestReleasePleaseLeavesTheTagToTheOwner(t *testing.T) {
	data, err := os.ReadFile("../../release-please-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Packages map[string]struct {
			SkipGitHubRelease bool `json:"skip-github-release"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Packages["."].SkipGitHubRelease {
		t.Fatal(`release-please-config.json: packages["."]["skip-github-release"] must be true`)
	}
}
