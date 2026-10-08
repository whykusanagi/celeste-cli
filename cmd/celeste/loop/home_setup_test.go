package loop

import (
	"testing"
)

// Aikido 806869780: Setup with no home directory fails instead of reading
// skills, hooks and permissions from the workspace's own ./.celeste.
func TestSetupFailsWithoutAHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	ws := t.TempDir()
	t.Chdir(ws)
	env, err := Setup(ModeAgent, testCfg(), ws, SetupOptions{Warn: func(string) {}})
	if err == nil {
		env.Close()
		t.Fatal("Setup succeeded with no home directory")
	}
}
