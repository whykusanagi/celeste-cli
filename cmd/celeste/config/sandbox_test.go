package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWS(t *testing.T, ws, body string) string {
	t.Helper()
	p := filepath.Join(ws, ".celeste", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadWorkspaceSandboxCanonicalBody(t *testing.T) {
	ws := t.TempDir()
	want := writeWS(t, ws, `{"model":"x","sandbox":{"writable":["/opt/cache"],"network":false,"enabled":false,"other":1}}`)
	s, path, body, err := LoadWorkspaceSandbox(ws)
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if s == nil || s.Enabled == nil || *s.Enabled || s.Network == nil || *s.Network || len(s.Writable) != 1 {
		t.Fatalf("sandbox = %+v", s)
	}
	if body != `{"enabled":false,"network":false,"writable":["/opt/cache"]}` {
		t.Fatalf("body = %s", body)
	}
	if !s.Loosens() {
		t.Fatal("enabled:false and writable loosen")
	}
}

func TestLoadWorkspaceSandboxAbsentOrInvalid(t *testing.T) {
	ws := t.TempDir()
	if s, _, _, err := LoadWorkspaceSandbox(ws); s != nil || err != nil {
		t.Fatalf("no file: %+v %v", s, err)
	}
	writeWS(t, ws, `{"model":"x"}`)
	if s, _, _, err := LoadWorkspaceSandbox(ws); s != nil || err != nil {
		t.Fatalf("no sandbox key: %+v %v", s, err)
	}
	writeWS(t, ws, `{"sandbox":`)
	if _, _, _, err := LoadWorkspaceSandbox(ws); err == nil {
		t.Fatal("malformed file: want an error")
	}
	writeWS(t, ws, `{"sandbox":{"enabled":"yes"}}`)
	if _, _, _, err := LoadWorkspaceSandbox(ws); err == nil {
		t.Fatal("wrong type: want an error")
	}
}

func TestSandboxLoosens(t *testing.T) {
	f, tr := false, true
	for _, c := range []struct {
		s    Sandbox
		want bool
	}{
		{Sandbox{}, false},
		{Sandbox{Enabled: &tr}, false},
		{Sandbox{Network: &f}, false},
		{Sandbox{Enabled: &f}, true},
		{Sandbox{Network: &tr}, true},
		{Sandbox{Writable: []string{"x"}}, true},
	} {
		if got := c.s.Loosens(); got != c.want {
			t.Errorf("%+v.Loosens() = %v, want %v", c.s, got, c.want)
		}
	}
}
