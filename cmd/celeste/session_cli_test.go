package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// W-C1: `celeste session list` listed nothing and exited 0; only --list
// worked. The list subcommand now lists, and an unknown word is an error
// instead of a silent no-op.
func TestSessionCLI_ListSubcommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	mgr := config.NewSessionManager()
	s := mgr.NewSession()
	s.Messages = append(s.Messages, config.SessionMessage{Role: "user", Content: "hello there"})
	require.NoError(t, mgr.Save(s))

	for _, args := range [][]string{{"list"}, {"--list"}, {}} {
		var out, errBuf bytes.Buffer
		assert.Equal(t, 0, sessionCLI(args, mgr, &out, &errBuf), args)
		assert.Contains(t, out.String(), "Saved Sessions (1)", args)
		assert.Contains(t, out.String(), s.ID, args)
		assert.Empty(t, errBuf.String(), args)
	}

	var out, errBuf bytes.Buffer
	assert.Equal(t, 2, sessionCLI([]string{"bogus"}, mgr, &out, &errBuf))
	assert.Empty(t, out.String())
	assert.Contains(t, errBuf.String(), `Unknown session command "bogus"`)
	assert.Contains(t, errBuf.String(), "Usage: celeste session")

	// The sessions are untouched by the bad command.
	list, err := mgr.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

// list, --load and --clear are separate actions: combining them is an
// error, so `celeste session list --clear` never deletes the sessions.
func TestSessionCLI_CombinedActionsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	mgr := config.NewSessionManager()
	s := mgr.NewSession()
	s.Messages = append(s.Messages, config.SessionMessage{Role: "user", Content: "keep me"})
	require.NoError(t, mgr.Save(s))

	for _, args := range [][]string{
		{"list", "--clear"}, {"--list", "--clear"}, {"list", "--load", s.ID},
		{"--load", s.ID, "--clear"}, {"list", "--list"},
	} {
		var out, errBuf bytes.Buffer
		assert.Equal(t, 2, sessionCLI(args, mgr, &out, &errBuf), args)
		assert.Contains(t, errBuf.String(), "one of list, --load or --clear", args)
		assert.Empty(t, out.String(), args)
	}
	list, err := mgr.List()
	require.NoError(t, err)
	assert.Len(t, list, 1, "a refused command deleted sessions")
}
