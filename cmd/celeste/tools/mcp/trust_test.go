package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

// The approval hash covers what decides what runs (transport, command,
// args, env and url) and nothing else.
func TestTrustHashCoversWhatRuns(t *testing.T) {
	base := ServerConfig{Transport: "stdio", Command: "srv", Args: []string{"-a"}, Env: map[string]string{"K": "v"}, URL: "https://h/x"}
	h := base.TrustHash()
	if h == "" || h != base.TrustHash() {
		t.Fatal("TrustHash is empty or unstable")
	}
	edits := map[string]func(*ServerConfig){
		"command":   func(c *ServerConfig) { c.Command = "other" },
		"args":      func(c *ServerConfig) { c.Args = []string{"-b"} },
		"env key":   func(c *ServerConfig) { c.Env = map[string]string{"LD_PRELOAD": "v"} },
		"env value": func(c *ServerConfig) { c.Env = map[string]string{"K": "w"} },
		"url":       func(c *ServerConfig) { c.URL = "https://evil/x" },
		"transport": func(c *ServerConfig) { c.Transport = "sse" },
	}
	for name, edit := range edits {
		c := base
		edit(&c)
		if c.TrustHash() == h {
			t.Errorf("editing the %s keeps the hash", name)
		}
	}
	same := base
	same.Enabled, same.Trusted, same.Origin, same.Dir = true, true, "/x", "/y"
	if same.TrustHash() != h {
		t.Error("flags, origin or dir change the hash")
	}
}

// The approval text shows the command and args, env names but not values,
// and a URL without its password or query.
func TestTrustSummary(t *testing.T) {
	c := ServerConfig{Transport: "stdio", Command: "srv\x1b", Args: []string{"--x"}, Env: map[string]string{"TOKEN": "ENV-SECRET"}, URL: "https://u:PW-SECRET@host/p?key=Q-SECRET"}
	s := c.TrustSummary()
	for _, want := range []string{`"srv\x1b"`, `"--x"`, "TOKEN", "host/p"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	for _, secret := range []string{"ENV-SECRET", "PW-SECRET", "Q-SECRET", "\x1b"} {
		if strings.Contains(s, secret) {
			t.Errorf("summary shows %q:\n%s", secret, s)
		}
	}
}

func TestIsGlobalConfig(t *testing.T) {
	home := t.TempDir()
	if !IsGlobalConfig(home, filepath.Join(home, ".claude", "mcp.json")) {
		t.Error("a home config is not global")
	}
	if IsGlobalConfig(home, filepath.Join(home, "repo", ".mcp.json")) || IsGlobalConfig("", ".celeste/mcp.json") {
		t.Error("a workspace config, or any config with no home, is global")
	}
}
