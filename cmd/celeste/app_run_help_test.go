package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// -h / --help on a subcommand that takes free text or acts on its first
// argument prints that subcommand's usage and runs nothing (#322): the
// help flag is never sent to the model, saved as a memory, or looked up as
// a skill, session or memory name.
func TestRun_SubcommandHelpPrintsUsage(t *testing.T) {
	for _, cmd := range []string{"message", "msg", "chat", "remember", "forget", "resume", "skill", "index"} {
		for _, flag := range []string{"-h", "--help", "-help"} {
			t.Run(cmd+" "+flag, func(t *testing.T) {
				r := &fakeRunner{}
				var out, errBuf bytes.Buffer
				code := run([]string{cmd, flag}, r, &out, &errBuf)
				assert.Equal(t, 0, code)
				assert.Empty(t, r.lastCall, "the subcommand ran")
				assert.Empty(t, r.lastMessage, "a message was sent")
				assert.Contains(t, out.String(), "Usage: celeste ")
				assert.Empty(t, errBuf.String())
			})
		}
	}
}

// The message usage names the subcommand, and help is only the first
// argument: words after the text still send, and -- sends text that
// starts with a help flag.
func TestRun_MessageHelpEdges(t *testing.T) {
	r := &fakeRunner{}
	var out, errBuf bytes.Buffer
	assert.Equal(t, 0, run([]string{"msg", "--help"}, r, &out, &errBuf))
	assert.Contains(t, out.String(), "Usage: celeste message <text>")

	r = &fakeRunner{}
	assert.Equal(t, 0, run([]string{"message", "what", "does", "--help", "do"}, r, &out, &errBuf))
	assert.Equal(t, "what does --help do", r.lastMessage)

	r = &fakeRunner{}
	assert.Equal(t, 0, run([]string{"message", "--", "--help"}, r, &out, &errBuf))
	assert.Equal(t, "message", r.lastCall)
	assert.Equal(t, "--help", r.lastMessage)

	r = &fakeRunner{}
	errBuf.Reset()
	assert.Equal(t, 1, run([]string{"message", "--"}, r, &out, &errBuf))
	assert.Contains(t, errBuf.String(), "Usage: celeste message <text>")
	assert.Empty(t, r.lastCall)

	// -- ends the help check on the other free-text subcommands too, and is
	// not passed on as text.
	for _, cmd := range []string{"remember", "forget", "resume", "skill"} {
		r = &fakeRunner{}
		assert.Equal(t, 0, run([]string{cmd, "--", "--help"}, r, &out, &errBuf), cmd)
		assert.Equal(t, cmd, r.lastCall)
		assert.Equal(t, []string{"--help"}, r.lastArgs, cmd)
	}
}

// Subcommands with their own flag parsing keep their own -h handling.
func TestRun_FlagSubcommandsKeepTheirHelp(t *testing.T) {
	for _, cmd := range []string{"agent", "config", "serve", "session", "skills", "init", "acp"} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		run([]string{cmd, "-h"}, r, &out, &errBuf)
		assert.Equal(t, cmd, r.lastCall)
		assert.Equal(t, []string{"-h"}, r.lastArgs)
	}
}
