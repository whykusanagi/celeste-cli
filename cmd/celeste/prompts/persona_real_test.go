package prompts

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// useRealPersona opens the committed persona with the real key, which only
// the owner's machine has (make persona-check; ruling 19). Without
// CELESTE_PERSONA_KEY_FILE the test is skipped, as in CI.
func useRealPersona(t *testing.T) {
	t.Helper()
	path := os.Getenv("CELESTE_PERSONA_KEY_FILE")
	if path == "" {
		t.Skip("set CELESTE_PERSONA_KEY_FILE to check the real persona (make persona-check)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read the persona key file") // never print the file or the error's details
	}
	sub, err := fs.Sub(personaFS, "persona")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(UsePersonaSource(sub, strings.TrimSpace(string(data))))
	if _, err := VerifyPersona(); err != nil {
		t.Fatalf("the committed persona does not verify with this key: %v", err)
	}
}

// #48's rule reaches the model (issue #173): it is baked into every profile
// with a voice (Task 2's collections/cli_conduct.md, ruling 4).
func TestRealPersonaCarriesTheToolHonestyRule(t *testing.T) {
	useRealPersona(t)
	const rule = "unless a tool actually returned that result this turn"
	for _, p := range []Profile{ProfileFull, ProfileSpine, ProfileLite} {
		if !strings.Contains(mustProfile(p).SystemPrompt, rule) {
			t.Errorf("%s lacks the tool-honesty rule", p)
		}
	}
	if off := mustProfile(ProfileOff).SystemPrompt; strings.Contains(off, rule) || !strings.HasPrefix(off, "Voice Boundary:") {
		t.Error("off should be the voice boundary rule only")
	}
}

// The public persona reuses the CLI's copy of the voice boundary rule;
// report (don't fail) if the container's wording has moved on.
func TestRealPersonaVoiceBoundaryMatchesThePublicOne(t *testing.T) {
	useRealPersona(t)
	if strings.TrimSpace(mustProfile(ProfileOff).SystemPrompt) != strings.TrimSpace(VoiceBoundary) {
		t.Log("WARNING: the container's voice boundary rule differs from prompts.VoiceBoundary; consider copying its wording into the public persona")
	}
}

// Profiles shrink in order, and lite's size against the spec's 1.5k target
// is reported, not enforced (ruling 1).
func TestRealPersonaSizes(t *testing.T) {
	useRealPersona(t)
	for i := 1; i < len(Profiles); i++ {
		big, small := mustProfile(Profiles[i-1]), mustProfile(Profiles[i])
		if small.Tokens() >= big.Tokens() {
			t.Errorf("%s (%d tokens) is not smaller than %s (%d)", small.Profile, small.Tokens(), big.Profile, big.Tokens())
		}
	}
	if lite := mustProfile(ProfileLite); lite.Tokens() > 1500 {
		t.Logf("WARNING: lite is ~%d tokens; the spec's target is <=1,500 (W5 ruling 1)", lite.Tokens())
	}
}

// The off level with the real persona is identity, honesty, then the sealed
// off profile, and carries nothing of the full profile (W7 ruling 5).
func TestRealPersonaOffLevel(t *testing.T) {
	useRealPersona(t)
	tempHome(t)
	got := Compose(ComposeOptions{Mode: ModeAgent, PersonaLevel: PersonaOff})
	want := publicIdentity + "\n\n" + publicHonesty + "\n\n" + mustProfile(ProfileOff).SystemPrompt
	if !strings.HasPrefix(got, want) {
		t.Fatal("off level is not identity + honesty + voice boundary") // never print persona text
	}
	if strings.Contains(got, "When I'm working in the terminal") {
		t.Error("off carries cli_conduct.md")
	}
}
