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

func TestRun_NoArgs_ShowsUsageAndTipWhenDefaultConfigExists(t *testing.T) {
	r := &fakeRunner{hasDefaultConfig: true}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run([]string{}, r, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.True(t, r.usageCalled)
	assert.Contains(t, out.String(), "Maybe you meant `celeste chat`?")
	assert.Empty(t, errBuf.String())
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
