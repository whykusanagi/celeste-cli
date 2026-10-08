package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Aikido 806780680, 806780676: no checkout in any workflow keeps the token
// in .git/config; none of these jobs pushes.
func TestWorkflowCheckoutsDoNotPersistCredentials(t *testing.T) {
	for _, name := range workflowNames(t) {
		_, wf := readWorkflow(t, name)
		for job, j := range wf.Jobs {
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/checkout") && s.With["persist-credentials"] != false {
					t.Errorf("%s job %s: checkout %q persists credentials", name, job, s.Name)
				}
			}
		}
	}
}

// Aikido 806869782: every action is pinned to a full commit SHA with its
// version in a trailing comment, so a moved upstream tag cannot change what
// runs. Dependabot keeps the pins current.
func TestWorkflowActionsArePinnedToCommitSHAs(t *testing.T) {
	pinned := regexp.MustCompile(`^\s*-?\s*uses:\s*[\w.-]+/[\w./-]+@[0-9a-f]{40}\s+#\s*v\d+\.\d+\.\d+\s*$`)
	uses := regexp.MustCompile(`^\s*-?\s*uses:\s*(\S+)`)
	for _, name := range workflowNames(t) {
		text, _ := readWorkflow(t, name)
		for i, line := range strings.Split(text, "\n") {
			m := uses.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./") {
				continue
			}
			if !pinned.MatchString(line) {
				t.Errorf("%s:%d: %q is not pinned to a commit SHA with a # vX.Y.Z comment", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func workflowNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no workflows found")
	}
	return names
}
