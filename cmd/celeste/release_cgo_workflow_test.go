package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflowFile struct {
	On   map[string]any `yaml:"on"`
	Jobs map[string]struct {
		If       string `yaml:"if"`
		Needs    any    `yaml:"needs"`
		RunsOn   any    `yaml:"runs-on"`
		Strategy struct {
			Matrix struct {
				Include []map[string]string `yaml:"include"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []struct {
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) (string, workflowFile) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), wf
}

// #376: release binaries are built with CGo on each target's own runner, so
// they carry the tree-sitter parsers, and every one is smoke-tested there.
func TestReleaseWorkflowBuildsTreeSitterOnNativeRunners(t *testing.T) {
	text, wf := readWorkflow(t, "release.yml")
	if strings.Contains(text, `CGO_ENABLED: "0"`) {
		t.Error(`release.yml still builds with CGO_ENABLED: "0"`)
	}
	build, ok := wf.Jobs["build"]
	if !ok {
		t.Fatal("release.yml has no build job")
	}
	want := map[string]string{
		"linux-amd64":   "ubuntu-22.04",
		"linux-arm64":   "ubuntu-22.04-arm",
		"darwin-amd64":  "macos-15-intel",
		"darwin-arm64":  "macos-15",
		"windows-amd64": "windows-latest",
	}
	got := map[string]string{}
	for _, inc := range build.Strategy.Matrix.Include {
		got[inc["target"]] = inc["runner"]
	}
	for target, runner := range want {
		if got[target] != runner {
			t.Errorf("build matrix: %s runs on %q, want %q", target, got[target], runner)
		}
	}
	if len(got) != len(want) {
		t.Errorf("build matrix targets = %v, want %v", got, want)
	}
	var cgo, smoke bool
	for _, s := range build.Steps {
		if s.Name == "Build binaries" && s.Env["CGO_ENABLED"] == "1" {
			cgo = true
		}
		if strings.Contains(s.Run, "index selfcheck") && strings.Contains(s.Run, "--version") && strings.Contains(s.Run, "persona verify") {
			smoke = true
		}
	}
	if !cgo {
		t.Error(`the build job's "Build binaries" step does not set CGO_ENABLED: "1"`)
	}
	if !smoke {
		t.Error("the build job does not smoke-test each binary (--version, index selfcheck, persona verify)")
	}
}

// #376: a manual run is a dry run. It builds and smoke-tests everything and
// never reaches the job that signs and publishes.
func TestReleaseWorkflowDryRunNeverPublishes(t *testing.T) {
	_, wf := readWorkflow(t, "release.yml")
	if _, ok := wf.On["workflow_dispatch"]; !ok {
		t.Error("release.yml has no workflow_dispatch dry run")
	}
	if _, ok := wf.On["push"]; !ok {
		t.Error("release.yml no longer runs on tag pushes")
	}
	var publishers []string
	for name, job := range wf.Jobs {
		for _, s := range job.Steps {
			if strings.HasPrefix(s.Uses, "softprops/action-gh-release") || strings.Contains(s.Run, "gpg --detach-sign") || strings.Contains(s.Name, "GPG") {
				publishers = append(publishers, name)
				if !strings.Contains(job.If, "github.event_name == 'push'") || !strings.Contains(job.If, "refs/tags/v") {
					t.Errorf("job %s signs or publishes but its if is %q: a dry run would reach it", name, job.If)
				}
				if job.Needs != "build" {
					t.Errorf("job %s needs %v, want build", name, job.Needs)
				}
			}
		}
	}
	if len(publishers) == 0 {
		t.Error("release.yml has no publish job")
	}
}

// #376: CI runs the tree-sitter tests with CGo on Linux PRs and still builds
// and tests the CGO_ENABLED=0 regex fallback.
func TestCIRunsTreeSitterAndThePureGoFallback(t *testing.T) {
	_, wf := readWorkflow(t, "ci.yml")
	var treeSitter, pureGo bool
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if strings.Contains(s.Run, "TestTreeSitterSelfCheckPassesWithCgo") {
				treeSitter = true
			}
			if s.Env["CGO_ENABLED"] == "0" && strings.Contains(s.Run, "go test") {
				pureGo = true
			}
		}
	}
	if !treeSitter {
		t.Error("ci.yml never checks that the CGo tree-sitter tests ran")
	}
	if !pureGo {
		t.Error("ci.yml never tests a CGO_ENABLED=0 build")
	}
}
