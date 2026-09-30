package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// #151: `celeste -config local models` used to chat the word "models". The
// words people guess for a command (models, model, status) error with a
// hint; nothing else does.
func TestRun_GuessedWordErrorsWithHint(t *testing.T) {
	for _, tc := range []struct {
		args []string
		word string
		hint string
	}{
		{[]string{"models"}, "models", "Did you mean: celeste providers"},
		{[]string{"-config", "local", "models"}, "models", "Did you mean: celeste providers"},
		{[]string{"model"}, "model", "Did you mean: celeste providers"},
		{[]string{"status"}, "status", "Did you mean: celeste config"},
	} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 1, run(tc.args, r, &out, &errBuf), tc.args)
		assert.Empty(t, r.lastCall, "nothing may run for %v", tc.args)
		assert.Contains(t, errBuf.String(), `Unknown command "`+tc.word+`".`, tc.args)
		assert.Contains(t, errBuf.String(), tc.hint, tc.args)
		assert.Contains(t, errBuf.String(), "celeste message "+tc.word, tc.args)
	}
}

// Every other lone word is a message, exactly as before #151: no typo
// matching, so near-misses of a command or a guessed word chat too.
func TestRun_OtherLoneWordIsAMessage(t *testing.T) {
	for _, word := range []string{"hello", "hi", "explain", "thanks", "what", "cat", "report", "modles", "stauts", "confg", "provders"} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 0, run([]string{word}, r, &out, &errBuf), word)
		assert.Equal(t, "message", r.lastCall, word)
		assert.Equal(t, word, r.lastMessage, word)
		assert.Empty(t, errBuf.String(), word)
	}
}

// Several words, a quoted sentence, or a word with capitals or punctuation
// are still messages.
func TestRun_NonCommandTextIsStillAMessage(t *testing.T) {
	for _, args := range [][]string{{"hello", "celeste"}, {"what models do you have?"}, {"Hello!"}, {"hi?"}, {"Models"}, {"models", "please"}} {
		r := &fakeRunner{}
		var out, errBuf bytes.Buffer
		assert.Equal(t, 0, run(args, r, &out, &errBuf), args)
		assert.Equal(t, "message", r.lastCall, args)
		assert.Empty(t, errBuf.String(), args)
	}
}
