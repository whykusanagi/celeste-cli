package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeRunner struct {
	hasDefaultConfig bool
	usageCalled      bool
	lastCall         string
	lastArgs         []string
	lastMessage      string
}

func (f *fakeRunner) PrintUsage()            { f.usageCalled = true }
func (f *fakeRunner) HasDefaultConfig() bool { return f.hasDefaultConfig }
func (f *fakeRunner) RunChat()               { f.lastCall = "chat" }
func (f *fakeRunner) RunConfig(args []string) {
	f.lastCall = "config"
	f.lastArgs = args
}
func (f *fakeRunner) RunSingleMessage(message string) {
	f.lastCall = "message"
	f.lastMessage = message
}
func (f *fakeRunner) RunContext(args []string) {
	f.lastCall = "context"
	f.lastArgs = args
}
func (f *fakeRunner) RunStats(args []string) {
	f.lastCall = "stats"
	f.lastArgs = args
}
func (f *fakeRunner) RunExport(args []string) {
	f.lastCall = "export"
	f.lastArgs = args
}
func (f *fakeRunner) RunSkill(args []string) {
	f.lastCall = "skill"
	f.lastArgs = args
}
func (f *fakeRunner) RunWalletMonitor(args []string) {
	f.lastCall = "wallet-monitor"
	f.lastArgs = args
}
func (f *fakeRunner) RunSkills(args []string) {
	f.lastCall = "skills"
	f.lastArgs = args
}
func (f *fakeRunner) RunProviders(args []string) {
	f.lastCall = "providers"
	f.lastArgs = args
}
func (f *fakeRunner) RunSession(args []string) {
	f.lastCall = "session"
	f.lastArgs = args
}
func (f *fakeRunner) RunCollections(args []string) {
	f.lastCall = "collections"
	f.lastArgs = args
}
func (f *fakeRunner) RunAgent(args []string) {
	f.lastCall = "agent"
	f.lastArgs = args
}
func (f *fakeRunner) RunInit(args []string) {
	f.lastCall = "init"
	f.lastArgs = args
}
func (f *fakeRunner) RunGrimoire(args []string) {
	f.lastCall = "grimoire"
	f.lastArgs = args
}
func (f *fakeRunner) RunIndex(args []string) {
	f.lastCall = "index"
	f.lastArgs = args
}
func (f *fakeRunner) RunServe(args []string) {
	f.lastCall = "serve"
	f.lastArgs = args
}
func (f *fakeRunner) RunCosts(args []string) {
	f.lastCall = "costs"
	f.lastArgs = args
}
func (f *fakeRunner) RunMemories(args []string) {
	f.lastCall = "memories"
	f.lastArgs = args
}
func (f *fakeRunner) RunRemember(args []string) {
	f.lastCall = "remember"
	f.lastArgs = args
}
func (f *fakeRunner) RunForget(args []string) {
	f.lastCall = "forget"
	f.lastArgs = args
}
func (f *fakeRunner) RunResume(args []string) {
	f.lastCall = "resume"
	f.lastArgs = args
}
func (f *fakeRunner) RunPlan(args []string) {
	f.lastCall = "plan"
	f.lastArgs = args
}
func (f *fakeRunner) RunRevert(args []string) {
	f.lastCall = "revert"
	f.lastArgs = args
}
func (f *fakeRunner) RunMCP(args []string) {
	f.lastCall = "mcp"
	f.lastArgs = args
}

func TestRun_NoArgs_LaunchesChatDirectly(t *testing.T) {
	r := &fakeRunner{hasDefaultConfig: true}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Equal(t, "chat", r.lastCall, "no args should launch chat TUI directly")
}

func TestRun_MessageWithoutBody_ReturnsError(t *testing.T) {
	r := &fakeRunner{}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{"message"}, r, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "Usage: celeste message <text>")
	assert.Empty(t, r.lastCall)
}

func TestRun_DispatchesKnownCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCall string
		wantArgs []string
	}{
		{name: "chat", args: []string{"chat"}, wantCall: "chat"},
		{name: "config", args: []string{"config", "--show"}, wantCall: "config", wantArgs: []string{"--show"}},
		{name: "context", args: []string{"context", "--show"}, wantCall: "context", wantArgs: []string{"--show"}},
		{name: "stats", args: []string{"stats", "--session"}, wantCall: "stats", wantArgs: []string{"--session"}},
		{name: "export", args: []string{"export", "--format", "json"}, wantCall: "export", wantArgs: []string{"--format", "json"}},
		{name: "skill", args: []string{"skill", "get_weather"}, wantCall: "skill", wantArgs: []string{"get_weather"}},
		{name: "wallet", args: []string{"wallet-monitor", "status"}, wantCall: "wallet-monitor", wantArgs: []string{"status"}},
		{name: "skills", args: []string{"skills", "--list"}, wantCall: "skills", wantArgs: []string{"--list"}},
		{name: "providers", args: []string{"providers", "current"}, wantCall: "providers", wantArgs: []string{"current"}},
		{name: "session", args: []string{"session", "--list"}, wantCall: "session", wantArgs: []string{"--list"}},
		{name: "collections", args: []string{"collections", "list"}, wantCall: "collections", wantArgs: []string{"list"}},
		{name: "agent", args: []string{"agent", "--goal", "do work"}, wantCall: "agent", wantArgs: []string{"--goal", "do work"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			var out bytes.Buffer
			var errBuf bytes.Buffer

			code := run(tt.args, r, &out, &errBuf)
			assert.Equal(t, 0, code)
			assert.Equal(t, tt.wantCall, r.lastCall)
			assert.Equal(t, tt.wantArgs, r.lastArgs)
			assert.Empty(t, errBuf.String())
		})
	}
}

func TestRun_VersionAndHelp(t *testing.T) {
	r := &fakeRunner{}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{"--version"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "Celeste CLI")
	assert.False(t, r.usageCalled)

	out.Reset()
	code = run([]string{"help"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.True(t, r.usageCalled)
}

func TestRun_UnknownCommandFallsBackToMessage(t *testing.T) {
	r := &fakeRunner{}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{"hello", "celeste"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Equal(t, "message", r.lastCall)
	assert.Equal(t, "hello celeste", r.lastMessage)
}

func TestRun_ConfigFlagParsing(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantConfig string
		wantMode   string
		wantClaw   int
		wantCall   string
	}{
		{
			name:       "spaced flag",
			args:       []string{"-config", "openai", "chat"},
			wantConfig: "openai",
			wantMode:   "",
			wantClaw:   0,
			wantCall:   "chat",
		},
		{
			name:       "equals flag",
			args:       []string{"-config=grok", "chat"},
			wantConfig: "grok",
			wantMode:   "",
			wantClaw:   0,
			wantCall:   "chat",
		},
		{
			name:       "mode and claw flags",
			args:       []string{"-mode", "claw", "-claw-max-iterations", "6", "chat"},
			wantConfig: "",
			wantMode:   "claw",
			wantClaw:   6,
			wantCall:   "chat",
		},
		{
			name:       "equals mode and claw flags",
			args:       []string{"-mode=claw", "-claw-max-iterations=3", "chat"},
			wantConfig: "",
			wantMode:   "claw",
			wantClaw:   3,
			wantCall:   "chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			var out bytes.Buffer
			var errBuf bytes.Buffer

			code := run(tt.args, r, &out, &errBuf)
			assert.Equal(t, 0, code)
			assert.Equal(t, tt.wantCall, r.lastCall)
			assert.Equal(t, tt.wantConfig, configName)
			assert.Equal(t, tt.wantMode, runtimeModeOverride)
			assert.Equal(t, tt.wantClaw, clawMaxToolIterationsOverride)
		})
	}
}

func TestRun_ConfigNameResetsPerInvocation(t *testing.T) {
	r := &fakeRunner{}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{"-config", "openai", "chat"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Equal(t, "openai", configName)
	assert.Empty(t, runtimeModeOverride)
	assert.Equal(t, 0, clawMaxToolIterationsOverride)

	r = &fakeRunner{}
	code = run([]string{"-mode", "claw", "-claw-max-iterations", "5", "chat"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Empty(t, configName)
	assert.Equal(t, "claw", runtimeModeOverride)
	assert.Equal(t, 5, clawMaxToolIterationsOverride)

	r = &fakeRunner{}
	code = run([]string{"chat"}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Empty(t, configName)
	assert.Empty(t, runtimeModeOverride)
	assert.Equal(t, 0, clawMaxToolIterationsOverride)
}

// CommitSHA[:8] panicked on any stamp shorter than 8 characters and silently
// mangled readable local stamps: `make install` stamps something like
// "4078dec-dirty", which sliced to "4078dec-" — losing the dirty marker and
// leaving a trailing hyphen that looks like corruption. Release builds stamp a
// full 40-char SHA and DO want abbreviating.
func TestShortCommit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "a1b2c3d4"}, // release: full SHA abbreviates
		{"4078dec-dirty", "4078dec-dirty"},                       // local: kept whole, dirty visible
		{"4078dec", "4078dec"},                                   // short sha kept whole
		{"abc", "abc"},                                           // would have PANICKED
		{"", ""},                                                 // would have PANICKED
		{"dev", "dev"},
	}
	for _, c := range cases {
		if got := shortCommit(c.in); got != c.want {
			t.Errorf("shortCommit(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// len(sha)==40 alone was a weak discriminator: a 40-character value that is not
// a SHA got truncated to 8 misleading characters — the same class of bug this
// helper exists to fix, just on a rarer input. Require hex as well.
func TestShortCommit_FortyCharNonSHA(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"40-char describe stamp is not a SHA",
			"v1.14.0-10-g4078dec-dirty-extra123456789",
			"v1.14.0-10-g4078dec-dirty-extra123456789",
		},
		{
			"40-char tag is not a SHA",
			"release-2026-08-08-build-metadata-abcdef",
			"release-2026-08-08-build-metadata-abcdef",
		},
		{
			"40-char hex IS a SHA and still abbreviates",
			"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
			"a1b2c3d4",
		},
		{
			"40-char uppercase hex is still a SHA",
			"A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F8A9B0",
			"A1B2C3D4",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shortCommit(c.in); got != c.want {
				t.Errorf("shortCommit(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
