package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Switching profiles replaces baseConfig; the jev_prune decision must follow
// it, or excerpts keep going to TypeSafe after shadow mode is turned off.
func TestJevShadowFollowsProfileSwitch(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	a := &TUIClientAdapter{baseConfig: &config.Config{JevPrune: "shadow"}}
	if a.jevShadow() == nil {
		t.Fatal("shadow profile: want a client")
	}
	a.baseConfig = &config.Config{}
	if a.jevShadow() != nil {
		t.Error("after switching to a profile without jev_prune, shadow mode must be off")
	}
	a.baseConfig = &config.Config{JevPrune: "shadow"}
	if a.jevShadow() == nil {
		t.Error("switching back to a shadow profile must turn it on again")
	}
}
