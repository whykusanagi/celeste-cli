package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// #151: `celeste -config local models` used to chat the word "models".
func TestRun_LoneUnknownWordErrors(t *testing.T) {
	for _, args := range [][]string{{"models"}, {"-config", "local", "models"}} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		code := run(args, r, &out, &errBuf)
		assert.Equal(t, 1, code, args)
		assert.Empty(t, r.lastCall, "nothing may run for %v", args)
		assert.Contains(t, errBuf.String(), `Unknown command "models".`)
		assert.Contains(t, errBuf.String(), "Did you mean: celeste providers")
		assert.Contains(t, errBuf.String(), "celeste message models")
	}
}

func TestRun_LoneTypoSuggestsClosestCommand(t *testing.T) {
	for typo, want := range map[string]string{"provders": "providers", "agnet": "agent", "reusme": "resume"} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 1, run([]string{typo}, r, &out, &errBuf))
		assert.Contains(t, errBuf.String(), "Did you mean: celeste "+want+"\n", typo)
	}
}

// A lone word that resembles no command is a message, exactly as before
// #151: only guessed words and near-misses of a command error.
func TestRun_LoneWordWithoutMatch(t *testing.T) {
	for _, word := range []string{"hello", "hi", "explain", "thanks", "why", "help-me"} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 0, run([]string{word}, r, &out, &errBuf), word)
		assert.Equal(t, "message", r.lastCall, word)
		assert.Equal(t, word, r.lastMessage, word)
		assert.Empty(t, errBuf.String(), word)
	}
}

// Typos of the guessed words get the guessed word's hint.
func TestRun_LoneMisfireTypoGetsHint(t *testing.T) {
	for typo, want := range map[string]string{
		"modles": "Did you mean: celeste providers",
		"stauts": "Did you mean: celeste config",
		"confg":  "Did you mean: celeste config\n",
	} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 1, run([]string{typo}, r, &out, &errBuf), typo)
		assert.Empty(t, r.lastCall, "nothing may run for %s", typo)
		assert.Contains(t, errBuf.String(), want, typo)
		assert.Contains(t, errBuf.String(), "celeste message "+typo, typo)
	}
}

func TestRun_StatusErrorsWithHint(t *testing.T) {
	r := &fakeRunner{}
	var out, errBuf bytes.Buffer
	assert.Equal(t, 1, run([]string{"status"}, r, &out, &errBuf))
	assert.Contains(t, errBuf.String(), "Did you mean: celeste config")
}

// Several words, a quoted sentence, or a word with capitals or punctuation
// are still messages.
func TestRun_NonCommandTextIsStillAMessage(t *testing.T) {
	for _, args := range [][]string{{"hello", "celeste"}, {"what models do you have?"}, {"Hello!"}, {"hi?"}} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 0, run(args, r, &out, &errBuf), args)
		assert.Equal(t, "message", r.lastCall, args)
		assert.Empty(t, errBuf.String(), args)
	}
}

// Every word in knownCommands reaches a real command, never the
// unknown-command error or the message fallback.
func TestKnownCommandsDispatch(t *testing.T) {
	for _, c := range knownCommands {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		run([]string{c, "arg"}, r, &out, &errBuf)
		assert.NotContains(t, errBuf.String(), "Unknown command", c)
		if c != "message" {
			assert.Empty(t, r.lastMessage, "%s fell through to a message", c)
		}
	}
}
