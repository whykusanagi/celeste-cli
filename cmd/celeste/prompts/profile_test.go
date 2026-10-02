package prompts

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt/personacrypttest"
)

// useTestPersona installs the synthetic persona sealed under the public
// test key for one test (ruling 19). Tests that call it must not be
// parallel: the persona is process-wide.
func useTestPersona(t testing.TB) {
	t.Helper()
	t.Cleanup(UsePersonaSource(personacrypttest.FS(VoiceBoundary), personacrypttest.Key))
}

// tempHome points HOME (and USERPROFILE, for Windows) at a fresh dir, so
// no test reads the developer's ~/.celeste.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// personaHome is tempHome plus the test persona.
func personaHome(t *testing.T) string {
	t.Helper()
	home := tempHome(t)
	useTestPersona(t)
	return home
}

// captureLog collects the standard logger's output for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&b)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &b
}

// publicFull is the public persona's full, spine and lite text (rulings
// 17, 24).
var publicFull = publicIdentity + "\n\n" + publicHonesty + "\n\n" + VoiceBoundary

// Ruling 24: the public persona keeps #48's rule; off stays the voice
// boundary rule alone, like the official off.
func TestPublicPersonaCarriesTheHonestyRule(t *testing.T) {
	const rule = "unless a tool actually returned that result this turn"
	set := publicPersona(personacrypt.ErrNoKey)
	for _, p := range []Profile{ProfileFull, ProfileSpine, ProfileLite} {
		if !strings.Contains(set.profiles[p].SystemPrompt, rule) {
			t.Errorf("public %s lacks the honesty rule", p)
		}
	}
	if set.profiles[ProfileOff].SystemPrompt != VoiceBoundary {
		t.Error("public off should be the voice boundary rule alone")
	}
}

// A go test binary has no key: it runs the public persona, says where the
// full one ships, and warns about nothing (Review Focus 7).
func TestPersonaNoKeyRunsThePublicPersona(t *testing.T) {
	if personaKey != "" {
		t.Skip("this test binary was built with a persona key")
	}
	tempHome(t)
	logs := captureLog(t)
	for _, p := range Profiles {
		want := publicFull
		if p == ProfileOff {
			want = VoiceBoundary
		}
		if pp := mustProfile(p); !pp.Public || pp.SystemPrompt != want {
			t.Errorf("%s: public %v, prompt %.60q", p, pp.Public, pp.SystemPrompt)
		}
	}
	if n := PersonaNotice(); !strings.Contains(n, "public persona") || !strings.Contains(n, "official release") {
		t.Errorf("notice = %q", n)
	}
	if _, err := VerifyPersona(); !errors.Is(err, personacrypt.ErrNoKey) {
		t.Errorf("VerifyPersona = %v, want ErrNoKey", err)
	}
	if strings.Contains(logs.String(), "[persona]") {
		t.Errorf("a build without a key should not warn: %q", logs.String())
	}
}

// The test persona decrypts: every profile is its plaintext, none is
// public, there is no notice, and VerifyPersona passes.
func TestPersonaRoundTrip(t *testing.T) {
	personaHome(t)
	plain := personacrypttest.Plaintexts(VoiceBoundary)
	for _, p := range Profiles {
		want, err := parseProfile(plain[string(p)], p)
		if err != nil {
			t.Fatal(err)
		}
		pp := mustProfile(p)
		if pp.Public || pp.SystemPrompt != want.SystemPrompt {
			t.Errorf("%s did not decrypt to its plaintext", p)
		}
		if !strings.HasSuffix(pp.SystemPrompt, VoiceBoundary) {
			t.Errorf("%s does not end with the voice boundary rule", p)
		}
	}
	if n := PersonaNotice(); n != "" {
		t.Errorf("notice %q with the persona decrypted", n)
	}
	summary, err := VerifyPersona()
	if err != nil || !strings.Contains(summary, "official persona") || !strings.Contains(summary, personacrypttest.CoreCommit[:12]) {
		t.Fatalf("VerifyPersona = %q, %v", summary, err)
	}
	if strings.Contains(summary, personacrypttest.Key) {
		t.Fatal("the summary carries the key")
	}
}

// brokenPersona is the test persona with one thing wrong, the key it is
// opened with, and the reason the loader must give.
type brokenPersona struct {
	name   string
	key    string
	mutate func(fstest.MapFS)
	reason error
}

// editSource rewrites the SOURCE.json inside m.
func editSource(m fstest.MapFS, edit func(*personacrypt.Source)) {
	src, err := personacrypt.ParseSource(m["SOURCE.json"].Data)
	if err != nil {
		panic(err)
	}
	edit(src)
	m["SOURCE.json"] = &fstest.MapFile{Data: src.JSON()}
}

func flip(m fstest.MapFS, name string) {
	data := bytes.Clone(m[name].Data)
	data[len(data)-1] ^= 1
	m[name] = &fstest.MapFile{Data: data}
}

func brokenPersonas() []brokenPersona {
	key := personacrypttest.Key
	spine := personacrypt.FileName("spine")
	return []brokenPersona{
		{"wrong key", strings.Repeat("ab", 32), nil, personacrypt.ErrWrongKey},
		{"short key", key[:63], nil, personacrypt.ErrMalformedKey},
		{"non-hex key", strings.Repeat("zz", 32), nil, personacrypt.ErrMalformedKey},
		{"flipped bit", key, func(m fstest.MapFS) { flip(m, spine) }, personacrypt.ErrDamaged},
		{"flipped bit, record updated", key, func(m fstest.MapFS) {
			flip(m, spine)
			editSource(m, func(s *personacrypt.Source) { s.Profiles[1].CiphertextSHA256 = personacrypt.SHA256Hex(m[spine].Data) })
		}, personacrypt.ErrDamaged},
		{"swapped files, records swapped too", key, func(m fstest.MapFS) {
			full := personacrypt.FileName("full")
			m[full], m[spine] = m[spine], m[full]
			editSource(m, func(s *personacrypt.Source) {
				a, b := s.Profiles[0], s.Profiles[1]
				s.Profiles[0], s.Profiles[1] = b, a
				s.Profiles[0].Profile, s.Profiles[0].File = a.Profile, a.File
				s.Profiles[1].Profile, s.Profiles[1].File = b.Profile, b.File
			})
		}, personacrypt.ErrDamaged},
		{"edited pin", key, func(m fstest.MapFS) {
			editSource(m, func(s *personacrypt.Source) { s.CoreCommit = strings.Repeat("3", 40) })
		}, personacrypt.ErrDamaged},
		{"missing file", key, func(m fstest.MapFS) { delete(m, personacrypt.FileName("lite")) }, personacrypt.ErrDamaged},
		{"garbled SOURCE.json", key, func(m fstest.MapFS) { m["SOURCE.json"] = &fstest.MapFile{Data: []byte("{")} }, personacrypt.ErrDamaged},
	}
}

func installBroken(t *testing.T, bp brokenPersona) {
	t.Helper()
	tempHome(t)
	fsys := personacrypttest.FS(VoiceBoundary)
	if bp.mutate != nil {
		bp.mutate(fsys)
	}
	t.Cleanup(UsePersonaSource(fsys, bp.key))
}

// Every broken persona fails closed: all four profiles public (never a
// mix), the reason, one warning, a notice, and no panic (Review Focus 7).
func TestPersonaFailsClosed(t *testing.T) {
	for _, bp := range brokenPersonas() {
		t.Run(bp.name, func(t *testing.T) {
			logs := captureLog(t)
			installBroken(t, bp)
			for _, p := range Profiles {
				if pp := mustProfile(p); !pp.Public {
					t.Errorf("%s is not the public persona", p)
				}
			}
			if _, err := VerifyPersona(); !errors.Is(err, bp.reason) {
				t.Errorf("reason %v, want %v", err, bp.reason)
			}
			if n := PersonaNotice(); !strings.Contains(n, "public persona") || !strings.Contains(n, "official release binary") {
				t.Errorf("notice = %q", n)
			}
			if got := strings.Count(logs.String(), "[persona] "+bp.reason.Error()); got != 1 {
				t.Errorf("want one warning, got %d: %q", got, logs.String())
			}
		})
	}
}

// Review Focus 6: whatever goes wrong, no key material reaches a log line,
// an error or a notice. Failure messages never print the key either.
func TestPersonaKeyNeverLeaks(t *testing.T) {
	for _, bp := range brokenPersonas() {
		t.Run(bp.name, func(t *testing.T) {
			logs := captureLog(t)
			installBroken(t, bp)
			_, err := VerifyPersona()
			out := logs.String() + "\n" + PersonaNotice() + "\n" + err.Error()
			for i, secret := range []string{bp.key, strings.ToUpper(bp.key), bp.key[:16], personacrypttest.Key[:16]} {
				if strings.Contains(out, secret) {
					t.Fatalf("output carries key material (check %d)", i)
				}
			}
		})
	}
}

// The public persona's bytes are pinned: the only persona text a golden may
// hold (ruling 19). Regenerate with
// go test ./cmd/celeste/prompts -run TestPublicPersonaGolden -update
func TestPublicPersonaGolden(t *testing.T) {
	set := publicPersona(personacrypt.ErrNoKey)
	for _, p := range []Profile{ProfileSpine, ProfileLite} {
		if set.profiles[p].SystemPrompt != set.profiles[ProfileFull].SystemPrompt {
			t.Errorf("public %s differs from public full", p)
		}
	}
	got := "== full, spine, lite ==\n" + set.profiles[ProfileFull].SystemPrompt + "\n== off ==\n" + set.profiles[ProfileOff].SystemPrompt + "\n"
	path := filepath.Join("testdata", "persona_public.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	// A Windows checkout may convert the golden to CRLF; compare the text.
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("the public persona differs from %s (run with -update if intended)\n--- got ---\n%s", path, got)
	}
}

// 1.x's ~/.celeste/celeste_essence.json is not read; one warning says so
// (Review Focus 3).
func TestLegacyEssenceIsIgnoredWithAWarning(t *testing.T) {
	home := personaHome(t)
	logs := captureLog(t)
	legacy := filepath.Join(home, ".celeste", "celeste_essence.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"version":"3.0.0","system_prompt":"You are Someone Else."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if got := mustProfile(ProfileFull).SystemPrompt; strings.Contains(got, "Someone Else") {
			t.Fatal("the legacy essence was used")
		}
	}
	if n := strings.Count(logs.String(), "celeste_essence.json is no longer read"); n != 1 {
		t.Fatalf("want one legacy warning, got %d:\n%s", n, logs.String())
	}
}

// HasPersonaKey makes a local build that carries the key Keyed, so it never
// replaces itself with a downloaded release (W5 ruling 25, audit #8 W3).
// It reports only whether a key was injected, not whether it decrypts.
func TestHasPersonaKey(t *testing.T) {
	prev := personaKey
	t.Cleanup(func() { personaKey = prev })
	for _, tc := range []struct {
		key  string
		want bool
	}{{"", false}, {" \n", false}, {"not hex", true}, {strings.Repeat("ab", 32) + "\n", true}} {
		personaKey = tc.key
		if got := HasPersonaKey(); got != tc.want {
			t.Errorf("HasPersonaKey with %d-byte key = %v, want %v", len(tc.key), got, tc.want)
		}
	}
}
