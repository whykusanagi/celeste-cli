package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
