package tui

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// projectDir is the session's workspace: the work dir the chat was started
// in, else the process's.
func (m AppModel) projectDir() string {
	if m.workDir != "" {
		return m.workDir
	}
	dir, _ := os.Getwd()
	return dir
}

// runInitCommand is /init [agents] (2.0 W4, ruling 7): it writes .grimoire
// with grimoire.Init and, with "agents", AGENTS.md with grimoire.InitAgents.
// An existing file is never overwritten; the reply says so and names it.
func runInitCommand(dir string, args []string) string {
	steps := []func(string) (string, error){grimoire.Init}
	if len(args) > 0 {
		if strings.ToLower(args[0]) != "agents" {
			return "Usage: /init [agents]"
		}
		steps = append(steps, grimoire.InitAgents)
	}
	var lines []string
	wrote := false
	for _, step := range steps {
		path, err := step(dir)
		switch {
		case errors.Is(err, fs.ErrExist):
			lines = append(lines, err.Error()+" (left as it is).")
		case err != nil:
			lines = append(lines, "init failed: "+err.Error())
		default:
			wrote = true
			lines = append(lines, "Created "+path+" (edit it to describe the project).")
		}
	}
	if wrote {
		lines = append(lines, "The new context applies from the next session.")
	}
	return strings.Join(lines, "\n")
}
