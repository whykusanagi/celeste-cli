package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// Sandbox is the "sandbox" object in ~/.celeste/config.json (the user's,
// trusted) and in a workspace's .celeste/config.json (the repository's:
// its tightening applies always, its loosening only once trusted). A nil
// field keeps the default.
type Sandbox struct {
	Enabled  *bool    `json:"enabled,omitempty"`  // run bash under the OS sandbox
	Writable []string `json:"writable,omitempty"` // more writable directories (relative: to the workspace)
	Network  *bool    `json:"network,omitempty"`  // false cuts the network
}

// Loosens reports whether s, coming from a repository, would weaken the
// sandbox: "enabled": false, "network": true or extra writable paths.
// Those apply only once the file is trusted; "enabled": true and
// "network": false tighten and always apply.
func (s *Sandbox) Loosens() bool {
	return s != nil && ((s.Enabled != nil && !*s.Enabled) || (s.Network != nil && *s.Network) || len(s.Writable) > 0)
}

// maxWorkspaceConfigBytes caps how much of a repository's config is read.
const maxWorkspaceConfigBytes = 1 << 20

// WorkspaceConfigPath is a workspace's own config file.
func WorkspaceConfigPath(workspace string) string {
	return filepath.Join(workspace, ".celeste", "config.json")
}

// LoadWorkspaceSandbox reads the "sandbox" object of workspace's
// .celeste/config.json. s is nil when the file or the key is absent. body
// is s as canonical JSON (sorted keys, no unknown fields), what trust
// approvals hash. The file's other keys are ignored.
func LoadWorkspaceSandbox(workspace string) (s *Sandbox, path, body string, err error) {
	path = WorkspaceConfigPath(workspace)
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, path, "", nil
	}
	if err != nil {
		return nil, path, "", err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, maxWorkspaceConfigBytes+1))
	if err != nil {
		return nil, path, "", err
	}
	if len(data) > maxWorkspaceConfigBytes {
		return nil, path, "", fmt.Errorf("%s is larger than 1 MiB", path)
	}
	var file struct {
		Sandbox *Sandbox `json:"sandbox"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, path, "", fmt.Errorf("%s: %w", path, err)
	}
	if file.Sandbox == nil {
		return nil, path, "", nil
	}
	b, err := canonicalJSON(file.Sandbox)
	if err != nil {
		return nil, path, "", err
	}
	return file.Sandbox, path, b, nil
}

// canonicalJSON marshals v with its object keys sorted.
func canonicalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	b, err = json.Marshal(m)
	return string(b), err
}

// sandboxInherit records the "sandbox" a named profile got from
// ~/.celeste/config.json: own is the profile's own object (nil when it had
// none), merged what Config.Sandbox was set to and ptr that pointer. While
// Config.Sandbox is still ptr with merged's values, saving the profile
// writes own back, so config.json's settings are never copied into it.
type sandboxInherit struct {
	own    *Sandbox
	ptr    *Sandbox
	merged Sandbox
}

// loadUserSandbox reads the "sandbox" object of the user's config.json at
// path. It is nil when the file or the key is absent.
func loadUserSandbox(path string) (*Sandbox, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Sandbox *Sandbox `json:"sandbox"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file.Sandbox, nil
}

// mergeSandbox is profile with each key it leaves unset taken from global:
// the profile wins key by key ("writable": [] in the profile means none).
func mergeSandbox(profile, global *Sandbox) *Sandbox {
	if global == nil {
		return profile
	}
	out := Sandbox{}
	if profile != nil {
		out = *profile
	}
	if out.Enabled == nil && global.Enabled != nil {
		v := *global.Enabled
		out.Enabled = &v
	}
	if out.Network == nil && global.Network != nil {
		v := *global.Network
		out.Network = &v
	}
	if out.Writable == nil && global.Writable != nil {
		out.Writable = append([]string{}, global.Writable...)
	}
	return &out
}

// inheritUserSandbox gives a named profile the user's sandbox settings
// from ~/.celeste/config.json (docs/SANDBOX.md), which LoadNamed otherwise
// never reads once a profile is active; keys the profile sets win. A
// config.json that cannot be read or parsed is skipped with a log line.
func (c *Config) inheritUserSandbox() {
	path := NamedConfigPath("")
	global, err := loadUserSandbox(path)
	if err != nil {
		log.Printf("[config] ignoring the sandbox settings in config.json: %v", err)
		return
	}
	if global == nil {
		return
	}
	own := c.Sandbox
	merged := mergeSandbox(own, global)
	if own != nil && sandboxEqual(*own, *merged) {
		return
	}
	c.Sandbox = merged
	c.sandboxFrom = &sandboxInherit{own: own, ptr: merged, merged: cloneSandbox(*merged)}
}

// savedSandbox is what saving c writes as "sandbox": the profile's own
// object while c.Sandbox is still the untouched inherited one, otherwise
// c.Sandbox.
func (c *Config) savedSandbox() *Sandbox {
	f := c.sandboxFrom
	if f != nil && c.Sandbox == f.ptr && c.Sandbox != nil && sandboxEqual(*c.Sandbox, f.merged) {
		return f.own
	}
	return c.Sandbox
}

func cloneSandbox(s Sandbox) Sandbox {
	if s.Enabled != nil {
		v := *s.Enabled
		s.Enabled = &v
	}
	if s.Network != nil {
		v := *s.Network
		s.Network = &v
	}
	if s.Writable != nil {
		s.Writable = append([]string{}, s.Writable...)
	}
	return s
}

func sandboxEqual(a, b Sandbox) bool {
	eq := func(x, y *bool) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	if !eq(a.Enabled, b.Enabled) || !eq(a.Network, b.Network) || (a.Writable == nil) != (b.Writable == nil) || len(a.Writable) != len(b.Writable) {
		return false
	}
	for i := range a.Writable {
		if a.Writable[i] != b.Writable[i] {
			return false
		}
	}
	return true
}
