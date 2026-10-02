package config

// Sandbox is the "sandbox" object in ~/.celeste/config.json (the user's,
// trusted) and in a workspace's .celeste/config.json (the repository's:
// its tightening applies always, its loosening only once trusted). A nil
// field keeps the default.
type Sandbox struct {
	Enabled  *bool    `json:"enabled,omitempty"`  // run bash under the OS sandbox
	Writable []string `json:"writable,omitempty"` // more writable directories (relative: to the workspace)
	Network  *bool    `json:"network,omitempty"`  // false cuts the network
}
