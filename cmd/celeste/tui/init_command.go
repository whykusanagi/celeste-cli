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

// runInitCommand is /init [agents] (2.0 W4, ruling 7): grimoire.RunInit,
// which `celeste init` runs too. Nothing is overwritten; the reply names
// each file written or left alone.
func runInitCommand(dir string, args []string) string {
	agents := false
	if len(args) > 0 {
		if strings.ToLower(args[0]) != "agents" {
			return "Usage: /init [agents]"
		}
		agents = true
	}
	lines, err := grimoire.RunInit(dir, agents)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		lines = append(lines, "init failed: "+err.Error())
	}
	return strings.Join(lines, "\n")
}
