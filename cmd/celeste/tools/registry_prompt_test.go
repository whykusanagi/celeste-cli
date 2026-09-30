package tools

import "testing"

// The chat's loop Gate reads the prompt at ask time (2.0 F2d), so a prompt
// installed after the Gate was built still answers.
func TestRegistryPromptReturnsTheInstalledPrompt(t *testing.T) {
	r := NewRegistry()
	if r.Prompt() != nil {
		t.Fatal("a new registry has a prompt")
	}
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse {
		return PermissionResponse{Decision: "allow_once"}
	})
	fn := r.Prompt()
	if fn == nil || fn(PermissionRequest{}).Decision != "allow_once" {
		t.Fatal("Prompt did not return the installed prompt")
	}
}

func TestRegistryDiscoveryModeReportsTheSwitch(t *testing.T) {
	r := NewRegistry()
	if r.DiscoveryMode() {
		t.Fatal("discovery is on by default")
	}
	r.SetDiscoveryMode(true)
	if !r.DiscoveryMode() {
		t.Fatal("DiscoveryMode did not report the switch")
	}
}
