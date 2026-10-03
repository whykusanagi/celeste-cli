package loop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/promptstest"
)

// personaCfg is testCfg with an explicit window (0 = the local default,
// 8,192: the fake endpoint is on 127.0.0.1).
func personaCfg(contextLimit int) *config.Config {
	return &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", ContextLimit: contextLimit}
}

// setupWith runs Setup on the test persona (realistic profile sizes,
// through the decrypt path; W5 ruling 19).
func setupWith(t *testing.T, mode Mode, cfg *config.Config, ws string) *Env {
	t.Helper()
	promptstest.Install(t)
	env, err := Setup(mode, cfg, ws, SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return env
}

// Spec W5 "Profiles by mode": chat full, agent spine; off on request.
func TestEnvPromptProfileByMode(t *testing.T) {
	setupHome(t)
	chat := setupWith(t, ModeChat, personaCfg(200000), t.TempDir())
	agent := setupWith(t, ModeAgent, personaCfg(200000), t.TempDir())
	for _, tc := range []struct {
		env  *Env
		opts PromptOptions
		want prompts.Profile
	}{
		{chat, PromptOptions{}, prompts.ProfileFull},
		{agent, PromptOptions{Contract: "C"}, prompts.ProfileSpine},
		{agent, PromptOptions{Contract: "C", Level: prompts.PersonaOff}, prompts.ProfileOff},
	} {
		if got := tc.env.SystemPrompt(tc.opts).Profile; got != tc.want {
			t.Errorf("%v %+v: profile %s, want %s", tc.env.Mode, tc.opts, got, tc.want)
		}
	}
}

// The Env's own window comes from its config (context_limit, else the
// model's or the local default); a run on another model passes its own.
func TestEnvPromptUsesTheResolvedWindow(t *testing.T) {
	setupHome(t)
	env := setupWith(t, ModeChat, personaCfg(0), t.TempDir())
	if got := env.SystemPrompt(PromptOptions{}).Profile; got != prompts.ProfileLite {
		t.Errorf("local default window: profile %s, want lite", got)
	}
	if got := env.SystemPrompt(PromptOptions{Window: 200000}).Profile; got != prompts.ProfileFull {
		t.Errorf("explicit 200k window: profile %s, want full", got)
	}
	child, err := env.Nested(NestedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if got := child.SystemPrompt(PromptOptions{}).Profile; got != prompts.ProfileLite {
		t.Errorf("a nested Env lost the window: profile %s, want lite", got)
	}
}

// Memories leave ProjectContext and follow git in the prompt (ruling 8);
// the /grimoire view keeps both.
func TestEnvPromptPutsMemoriesAfterTheGrimoire(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".grimoire"), "## Bindings\n- grimoire-probe\n")
	absWS, _ := filepath.Abs(ws)
	idx, _ := memories.LoadIndex(filepath.Join(memories.NewStore(absWS).BaseDir(), "MEMORY.md"))
	_ = idx.Add(memories.IndexEntry{Name: "memory-probe", File: "probe.md", Description: "probe"})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}
	env := setupWith(t, ModeChat, personaCfg(200000), ws)
	if strings.Contains(env.ProjectContext, "# Project Memories") || !strings.Contains(env.Memories, "memory-probe") {
		t.Fatalf("memories not split out: ProjectContext %q, Memories %q", env.ProjectContext, env.Memories)
	}
	if !strings.Contains(env.GrimoireContext, "grimoire-probe") || !strings.Contains(env.GrimoireContext, "memory-probe") {
		t.Fatalf("/grimoire view lost something: %q", env.GrimoireContext)
	}
	d := env.SystemPrompt(PromptOptions{}).Dynamic
	if g, m := strings.Index(d, "grimoire-probe"), strings.Index(d, "memory-probe"); g < 0 || m < g {
		t.Fatalf("memories (%d) must follow the grimoire (%d)", m, g)
	}
}
