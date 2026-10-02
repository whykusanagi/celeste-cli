package prompts

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt"
)

// Profile names one celeste-persona-container CLI build (W5, #173).
type Profile string

const (
	ProfileFull  Profile = "full"  // the whole tier-0 corpus: chat, MCP chat, ACP
	ProfileSpine Profile = "spine" // the agent-run cut
	ProfileLite  Profile = "lite"  // the small-window floor
	ProfileOff   Profile = "off"   // the voice boundary rule only: review and research lanes
)

// Profiles lists every profile, largest first.
var Profiles = []Profile{ProfileFull, ProfileSpine, ProfileLite, ProfileOff}

// PersonaProfile is one CLI build, the container's celeste_<profile>.json.
type PersonaProfile struct {
	Profile      Profile  `json:"profile"`
	Character    string   `json:"character"`
	SourceCommit string   `json:"source_commit"`
	Files        []string `json:"files"`
	Bytes        int      `json:"bytes"`
	ApproxTokens int      `json:"approx_tokens"`
	SystemPrompt string   `json:"system_prompt"`
	// Public marks the public persona (ruling 17).
	Public bool `json:"-"`
}

// Tokens estimates the profile's size the way the container does: UTF-8
// bytes / 4. Computed, not read, so a file can't misstate it.
func (p *PersonaProfile) Tokens() int { return len(p.SystemPrompt) / 4 }

// personaKey is the persona key, 64 hex characters. Only official release
// builds set it:
//
//	-ldflags "-X github.com/whykusanagi/celeste-cli/cmd/celeste/prompts.personaKey=…"
//
// (release.yml, from the CELESTE_PERSONA_KEY secret, with -trimpath so the
// toolchain does not copy it into the build info). Source builds leave it
// empty and run the public persona. Nothing may log, print or wrap it.
var personaKey string

// personaFS is the sealed persona (make sync-persona): SOURCE.json and the
// four celeste_<profile>.enc files. persona/LICENSE is not embedded.
//
//go:embed persona/SOURCE.json persona/*.enc
var personaFS embed.FS

// VoiceBoundary keeps the persona out of artifacts. The container ends
// every profile with this rule (off is only this rule), and the public
// persona ends with this copy of it.
const VoiceBoundary = `Voice Boundary:
Your voice, personality and the voice modulation below apply only to prose you address to the user. Code, code comments, commit messages, file contents, and tool-call arguments are written plainly and professionally: no persona voice, emotes, pet names, or stylised spelling. Where a tool's instructions and a voice instruction conflict, the tool's instructions win.`

// publicIdentity is the public persona's one line (ruling 17).
const publicIdentity = "You are Celeste, the AI companion in the celeste command-line tool, created by whyKusanagi."

// publicHonesty is #48's rule for the public persona (ruling 24): a
// behaviour rule, not personality, so source builds keep it. It shares
// "unless a tool actually returned that result this turn" with the
// official persona's wording.
const publicHonesty = "Never say that a file was written, audio was saved or any other action happened unless a tool actually returned that result this turn."

// publicPreamble is the public persona's identity line and honesty rule,
// the part before the voice boundary. The PersonaOff level (compose.go)
// starts with it too, in the official build as well.
const publicPreamble = publicIdentity + "\n\n" + publicHonesty

// personaSet is the four profiles a process runs with: all decrypted, or
// all public. Never a mix.
type personaSet struct {
	profiles          map[Profile]*PersonaProfile
	coreCommit, keyID string
	// reason is why the official persona is not active; nil when it is.
	reason error
}

var (
	// builtinPersona is this binary's persona, decided once per process,
	// before the first prompt is composed.
	builtinPersona = sync.OnceValue(func() *personaSet {
		sub, err := fs.Sub(personaFS, "persona")
		if err != nil {
			return publicPersona(personacrypt.ErrDamaged)
		}
		return loadPersona(sub, personaKey)
	})
	// personaOverride replaces builtinPersona in tests (UsePersonaSource).
	personaOverride atomic.Pointer[personaSet]
)

func currentPersona() *personaSet {
	if s := personaOverride.Load(); s != nil {
		return s
	}
	return builtinPersona()
}

// UsePersonaSource makes the process run the persona sealed in fsys
// (SOURCE.json and celeste_<profile>.enc at its root) under hexKey, and
// returns the function that restores the previous one. It exists for
// tests (promptstest.Install): every package can exercise decryption and
// realistic profile sizes with the public test key, never the real one.
func UsePersonaSource(fsys fs.FS, hexKey string) (restore func()) {
	prev := personaOverride.Swap(loadPersona(fsys, hexKey))
	return func() { personaOverride.Store(prev) }
}

// loadPersona decrypts all four profiles in fsys under hexKey, in memory.
// Any failure gives the public persona (ruling 17): nothing here panics,
// and no log line or error carries key material. A missing key is a source
// build, not a fault, so it is not logged.
func loadPersona(fsys fs.FS, hexKey string) *personaSet {
	set, err := decryptPersona(fsys, hexKey)
	if err == nil {
		return set
	}
	if !errors.Is(err, personacrypt.ErrNoKey) {
		log.Printf("[persona] %v; using the public persona", err)
	}
	return publicPersona(err)
}

// decryptPersona returns only personacrypt's fixed errors.
func decryptPersona(fsys fs.FS, hexKey string) (*personaSet, error) {
	key, err := personacrypt.ParseKey(hexKey)
	if err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(fsys, "SOURCE.json")
	if err != nil {
		return nil, personacrypt.ErrDamaged
	}
	src, err := personacrypt.ParseSource(data)
	if err != nil {
		return nil, personacrypt.ErrDamaged
	}
	if personacrypt.KeyID(key) != src.KeyID {
		return nil, personacrypt.ErrWrongKey
	}
	set := &personaSet{profiles: make(map[Profile]*PersonaProfile, len(Profiles)), coreCommit: src.CoreCommit, keyID: src.KeyID}
	for _, p := range Profiles {
		e, ok := src.Entry(string(p))
		if !ok {
			return nil, personacrypt.ErrDamaged
		}
		sealed, err := fs.ReadFile(fsys, e.File)
		if err != nil || len(sealed) != e.CiphertextBytes || personacrypt.SHA256Hex(sealed) != e.CiphertextSHA256 {
			return nil, personacrypt.ErrDamaged
		}
		plain, err := personacrypt.Open(key, sealed, personacrypt.AAD(string(p), src.CoreCommit))
		if err != nil || personacrypt.SHA256Hex(plain) != e.PlaintextSHA256 {
			return nil, personacrypt.ErrDamaged
		}
		pp, err := parseProfile(plain, p)
		if err != nil || pp.SourceCommit != src.CoreCommit {
			return nil, personacrypt.ErrDamaged
		}
		set.profiles[p] = pp
	}
	return set, nil
}

// publicPersona is the fallback set (ruling 17): the one-line identity, the
// honesty rule (ruling 24) and the voice boundary rule for full, spine and
// lite; the voice boundary rule alone for off.
func publicPersona(reason error) *personaSet {
	set := &personaSet{profiles: make(map[Profile]*PersonaProfile, len(Profiles)), reason: reason}
	for _, p := range Profiles {
		text := publicPreamble + "\n\n" + VoiceBoundary
		if p == ProfileOff {
			text = VoiceBoundary
		}
		set.profiles[p] = &PersonaProfile{Profile: p, Character: "Celeste", Bytes: len(text), ApproxTokens: len(text) / 4, SystemPrompt: text, Public: true}
	}
	return set
}

// parseProfile unmarshals and checks one decrypted build.
func parseProfile(data []byte, want Profile) (*PersonaProfile, error) {
	var pp PersonaProfile
	if err := json.Unmarshal(data, &pp); err != nil {
		return nil, fmt.Errorf("parse persona profile: %w", err)
	}
	if pp.Profile != want {
		return nil, fmt.Errorf("file is the %q profile, want %q", pp.Profile, want)
	}
	if strings.TrimSpace(pp.SystemPrompt) == "" {
		return nil, errors.New("no system_prompt")
	}
	return &pp, nil
}

// LoadProfile returns profile p: the official build when this binary can
// decrypt it, else the public persona. It fails only for an unknown name.
func LoadProfile(p Profile) (*PersonaProfile, error) {
	warnLegacyEssence()
	pp, ok := currentPersona().profiles[p]
	if !ok {
		return nil, fmt.Errorf("unknown persona profile %q", p)
	}
	return pp, nil
}

// mustProfile is LoadProfile for code paths with no error return. Only an
// unknown profile name, a programming error, can fail.
func mustProfile(p Profile) *PersonaProfile {
	pp, err := LoadProfile(p)
	if err != nil {
		panic(fmt.Sprintf("prompts: %v", err))
	}
	return pp
}

// PersonaNotice is the user-facing line for a binary that runs the public
// persona; "" when the official persona is active (ruling 17).
func PersonaNotice() string {
	reason := currentPersona().reason
	switch {
	case reason == nil:
		return ""
	case errors.Is(reason, personacrypt.ErrNoKey):
		return "Persona: this build runs Celeste's public persona. Her full persona ships only in official release binaries (github.com/whykusanagi/celeste-cli/releases)."
	default:
		return fmt.Sprintf("Persona: %v, so this build runs Celeste's public persona. Reinstall an official release binary (github.com/whykusanagi/celeste-cli/releases).", reason)
	}
}

// VerifyPersona reports whether this binary runs the official persona (W5
// ruling 20, `celeste persona verify`): a one-line summary when all four
// profiles decrypted, matched SOURCE.json and end with the off profile's
// voice boundary rule; otherwise the reason, one of personacrypt's errors.
func VerifyPersona() (string, error) {
	s := currentPersona()
	if s.reason != nil {
		return "", s.reason
	}
	off := s.profiles[ProfileOff].SystemPrompt
	sizes := make([]string, 0, len(Profiles))
	for _, p := range Profiles {
		pp := s.profiles[p]
		if !strings.HasSuffix(pp.SystemPrompt, off) {
			return "", fmt.Errorf("the %s profile does not end with the voice boundary rule", p)
		}
		sizes = append(sizes, fmt.Sprintf("%s ~%d", p, pp.Tokens()))
	}
	return fmt.Sprintf("official persona: core %.12s, key id %s; %s tokens", s.coreCommit, s.keyID, strings.Join(sizes, ", ")), nil
}

// warnLegacyEssence tells a 1.x user once that their essence file is no
// longer read (ruling 10).
func warnLegacyEssence() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	legacy := filepath.Join(home, ".celeste", "celeste_essence.json")
	if _, err := os.Stat(legacy); err == nil {
		warnOnce(fmt.Sprintf("[persona] %s is no longer read: celeste 2.0 builds the persona in, and there is no override. You can delete it; see MIGRATING-2.0.md", legacy))
	}
}

// warned holds the warnings already logged, so the per-message compose
// path doesn't repeat them.
var warned sync.Map

func warnOnce(msg string) {
	if _, seen := warned.LoadOrStore(msg, struct{}{}); !seen {
		log.Print(msg)
	}
}
