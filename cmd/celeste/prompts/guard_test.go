package prompts

import (
	"strings"
	"testing"
)

// The spec's own numbers: at the unknown-local default of 8,192 tokens chat
// gets lite; with a configured 65,536 window, chat gets full. The test
// persona's sizes match the real builds' (full ~10.5k, spine ~6k, lite ~3k).
func TestGuardSpecWindows(t *testing.T) {
	personaHome(t)
	for _, tc := range []struct {
		want   Profile
		window int
		got    Profile
	}{
		{ProfileFull, 8192, ProfileLite},
		{ProfileFull, 65536, ProfileFull},
		{ProfileSpine, 8192, ProfileLite},
		{ProfileSpine, 32768, ProfileSpine},
		{ProfileFull, 32768, ProfileSpine},
		{ProfileLite, 8192, ProfileLite},
	} {
		if pp, _ := selectProfile(tc.want, tc.window); pp.Profile != tc.got {
			t.Errorf("%s at %d: got %s, want %s", tc.want, tc.window, pp.Profile, tc.got)
		}
	}
}

// One level at a time: a window that fits spine but not full gets spine.
func TestGuardStepsOneLevelAtATime(t *testing.T) {
	personaHome(t)
	full, spine := mustProfile(ProfileFull), mustProfile(ProfileSpine)
	if pp, _ := selectProfile(ProfileFull, guardDivisor*full.Tokens()); pp.Profile != ProfileFull {
		t.Fatalf("a window of exactly 4x full got %s", pp.Profile)
	}
	w := guardDivisor * spine.Tokens()
	if full.Tokens() <= w/guardDivisor {
		t.Fatalf("precondition: full (%d tokens) must exceed a quarter of %d", full.Tokens(), w)
	}
	if pp, _ := selectProfile(ProfileFull, w); pp.Profile != ProfileSpine {
		t.Fatalf("got %s, want spine", pp.Profile)
	}
}

// Lite stays while it fits in half the window, even over a quarter (W5
// ruling 1: the built lite is ~3k tokens). Below that the floor is off:
// identity, the honesty rule and the voice boundary, never nothing.
func TestGuardFloorIsOff(t *testing.T) {
	personaHome(t)
	lite := mustProfile(ProfileLite).Tokens()
	if pp, _ := selectProfile(ProfileFull, offDivisor*lite); pp.Profile != ProfileLite {
		t.Fatalf("a window of 2x lite got %s, want lite", pp.Profile)
	}
	for _, want := range []Profile{ProfileFull, ProfileSpine, ProfileLite} {
		for _, window := range []int{offDivisor*lite - 1, 4096, 100, 1} {
			if pp, _ := selectProfile(want, window); pp.Profile != ProfileOff {
				t.Errorf("%s at %d tokens: got %s, want off", want, window, pp.Profile)
			}
		}
	}
}

// Off is chosen on purpose (reporting lanes) and never stepped; an unknown
// window (0 or less) disables the guard.
func TestGuardLeavesOffAndUnknownWindowsAlone(t *testing.T) {
	personaHome(t)
	if pp, n := selectProfile(ProfileOff, 100); pp.Profile != ProfileOff || n != "" {
		t.Errorf("off at 100: got %s, notice %q", pp.Profile, n)
	}
	for _, window := range []int{0, -1} {
		if pp, n := selectProfile(ProfileFull, window); pp.Profile != ProfileFull || n != "" {
			t.Errorf("full at %d: got %s, notice %q", window, pp.Profile, n)
		}
	}
}

// The notice names the profile in use and context_limit, is logged, and is
// returned once per window (window 8191 is used by no other test).
func TestGuardNoticeOnce(t *testing.T) {
	personaHome(t)
	logs := captureLog(t)
	_, first := selectProfile(ProfileFull, 8191)
	for _, want := range []string{"lite profile", "instead of full", "context_limit"} {
		if !strings.Contains(first, want) {
			t.Errorf("notice %q lacks %q", first, want)
		}
	}
	if !strings.Contains(logs.String(), "[persona] "+first) {
		t.Errorf("notice not logged: %q", logs.String())
	}
	if _, again := selectProfile(ProfileFull, 8191); again != "" {
		t.Errorf("notice repeated: %q", again)
	}
	if _, none := selectProfile(ProfileFull, 200000); none != "" {
		t.Errorf("no step, but a notice: %q", none)
	}
}

// Compose applies the guard (window 8193 is used by no other test).
func TestComposeAppliesTheGuard(t *testing.T) {
	composeEnv(t, false)
	p := Compose(ComposeOptions{Mode: ModeChat, Window: 8193})
	if p.Profile != ProfileLite || p.Static != mustProfile(ProfileLite).SystemPrompt || p.Notice == "" {
		t.Fatalf("got profile %s, notice %q", p.Profile, p.Notice)
	}
	if !strings.Contains(p.Dynamic, "Voice Modulation:") {
		t.Fatal("lite keeps the sliders")
	}
}

// A window too small for lite runs chat on off: identity, honesty and the
// voice boundary first, no sliders or user identity, but the chat rules and
// confirm mode stay, because the guard picks a size, not a lane (window
// 1500 is used by no other test).
func TestComposeGuardFloorKeepsTheChatRules(t *testing.T) {
	composeEnv(t, true)
	p := Compose(ComposeOptions{Mode: ModeChat, Window: 1500})
	if p.Profile != ProfileOff || p.Static != publicIdentity+"\n\n"+publicHonesty+"\n\n"+mustProfile(ProfileOff).SystemPrompt {
		t.Fatalf("got profile %s", p.Profile)
	}
	if p.Notice == "" || !strings.Contains(p.Notice, "voice boundary") {
		t.Fatalf("notice %q", p.Notice)
	}
	for _, want := range []string{"Task Execution Rules:", "Action Confirmation Mode:"} {
		if !strings.Contains(p.Dynamic, want) {
			t.Errorf("guard floor dropped %q", want)
		}
	}
	for _, banned := range []string{"Voice Modulation:", "Current User Identity:"} {
		if strings.Contains(p.Dynamic, banned) {
			t.Errorf("off carries %q", banned)
		}
	}
}

// The content prompt steps down with its window too.
func TestContentPromptUsesTheWindow(t *testing.T) {
	composeEnv(t, false)
	if got := GetContentPrompt(8192, "", "", "", ""); !strings.HasPrefix(got, mustProfile(ProfileLite).SystemPrompt) {
		t.Fatal("content prompt at 8,192 is not on lite")
	}
	if got := GetContentPrompt(0, "", "", "", ""); !strings.HasPrefix(got, mustProfile(ProfileFull).SystemPrompt) {
		t.Fatal("content prompt with an unknown window is not on full")
	}
}

// A fall to off says what is kept and the half-window rule, not the quarter
// rule, and never names the internal "off" profile (window 1700 is used by
// no other test).
func TestGuardNoticeAtTheFloor(t *testing.T) {
	personaHome(t)
	pp, n := selectProfile(ProfileFull, 1700)
	if pp.Profile != ProfileOff {
		t.Fatalf("got %s, want off", pp.Profile)
	}
	for _, want := range []string{"even the lite profile", "over half", "voice boundary", "context_limit"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice %q lacks %q", n, want)
		}
	}
	for _, banned := range []string{"quarter", "off profile"} {
		if strings.Contains(n, banned) {
			t.Errorf("notice %q says %q", n, banned)
		}
	}
}
