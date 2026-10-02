package personacrypt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Profiles lists the sealed profiles, in SOURCE.json's order.
var Profiles = []string{"full", "spine", "lite", "off"}

// FileName is a sealed profile's file name inside persona/.
func FileName(profile string) string { return "celeste_" + profile + ".enc" }

// SHA256Hex is the lowercase hex SHA-256 of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Source is persona/SOURCE.json: the pins the profiles were built at, the
// key's fingerprint, and every file's integrity record (W5 ruling 3). All
// of it can be checked without the key.
type Source struct {
	Format          string  `json:"format"`
	CoreCommit      string  `json:"core_commit"`
	ContainerCommit string  `json:"container_commit"`
	KeyID           string  `json:"key_id"`
	Profiles        []Entry `json:"profiles"`
}

// Entry is one sealed profile. PlaintextBytes and PlaintextSHA256 describe
// the container's celeste_<profile>.json; ApproxTokens is its
// system_prompt's bytes / 4, the container's own estimate.
type Entry struct {
	Profile          string `json:"profile"`
	File             string `json:"file"`
	CiphertextBytes  int    `json:"ciphertext_bytes"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	PlaintextBytes   int    `json:"plaintext_bytes"`
	PlaintextSHA256  string `json:"plaintext_sha256"`
	ApproxTokens     int    `json:"approx_tokens"`
}

var (
	fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hex16   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	hex64   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseSource reads SOURCE.json and checks that it is self-consistent:
// full-SHA pins, the four profiles in order under their own file names, and
// every ciphertext exactly Overhead bytes longer than its plaintext.
func ParseSource(data []byte) (*Source, error) {
	var s Source
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("SOURCE.json: %w", err)
	}
	if s.Format != Format {
		return nil, fmt.Errorf("SOURCE.json: format %q, want %q", s.Format, Format)
	}
	if !fullSHA.MatchString(s.CoreCommit) || !fullSHA.MatchString(s.ContainerCommit) {
		return nil, errors.New("SOURCE.json: pins must be full 40-character commit SHAs")
	}
	if !hex16.MatchString(s.KeyID) {
		return nil, errors.New("SOURCE.json: key_id must be 16 hex characters")
	}
	if len(s.Profiles) != len(Profiles) {
		return nil, fmt.Errorf("SOURCE.json: %d profiles, want %d", len(s.Profiles), len(Profiles))
	}
	for i, e := range s.Profiles {
		if e.Profile != Profiles[i] || e.File != FileName(e.Profile) {
			return nil, fmt.Errorf("SOURCE.json: entry %d is %q in %q, want %q in %q", i, e.Profile, e.File, Profiles[i], FileName(Profiles[i]))
		}
		if !hex64.MatchString(e.CiphertextSHA256) || !hex64.MatchString(e.PlaintextSHA256) ||
			e.PlaintextBytes <= 0 || e.CiphertextBytes != e.PlaintextBytes+Overhead || e.ApproxTokens < 0 {
			return nil, fmt.Errorf("SOURCE.json: the %s entry is inconsistent", e.Profile)
		}
	}
	return &s, nil
}

// Entry returns profile p's record.
func (s *Source) Entry(p string) (Entry, bool) {
	for _, e := range s.Profiles {
		if e.Profile == p {
			return e, true
		}
	}
	return Entry{}, false
}

// JSON is SOURCE.json's bytes: two-space indented, with a final newline.
func (s Source) JSON() []byte {
	b, _ := json.MarshalIndent(s, "", "  ") // strings and ints only: cannot fail
	return append(b, '\n')
}

// SealSet seals one container build. plain maps each profile to its
// celeste_<profile>.json bytes. prev and prevFiles are what is committed
// now (nil for a first sync): a profile whose plaintext hash, pin and key
// are unchanged keeps its committed ciphertext, so a sync that changes
// nothing rewrites nothing.
func SealSet(key []byte, coreCommit, containerCommit string, plain map[string][]byte, prev *Source, prevFiles map[string][]byte) (Source, map[string][]byte, error) {
	src := Source{Format: Format, CoreCommit: coreCommit, ContainerCommit: containerCommit, KeyID: KeyID(key)}
	files := make(map[string][]byte, len(Profiles))
	for _, p := range Profiles {
		pt, ok := plain[p]
		if !ok {
			return Source{}, nil, fmt.Errorf("the %s profile is missing", p)
		}
		var body struct {
			SystemPrompt string `json:"system_prompt"`
		}
		if err := json.Unmarshal(pt, &body); err != nil || body.SystemPrompt == "" {
			return Source{}, nil, fmt.Errorf("%s is not a built CLI profile (no system_prompt)", p)
		}
		e := Entry{Profile: p, File: FileName(p), PlaintextBytes: len(pt), PlaintextSHA256: SHA256Hex(pt), ApproxTokens: len(body.SystemPrompt) / 4}
		sealed := reusable(prev, prevFiles, e, coreCommit, src.KeyID)
		if sealed == nil {
			var err error
			if sealed, err = Seal(key, pt, AAD(p, coreCommit)); err != nil {
				return Source{}, nil, err
			}
		}
		e.CiphertextBytes, e.CiphertextSHA256 = len(sealed), SHA256Hex(sealed)
		src.Profiles = append(src.Profiles, e)
		files[e.File] = sealed
	}
	return src, files, nil
}

// reusable is the committed ciphertext for e when it still seals exactly
// this plaintext, at this pin, under this key; nil otherwise.
func reusable(prev *Source, prevFiles map[string][]byte, e Entry, coreCommit, keyID string) []byte {
	if prev == nil || prev.CoreCommit != coreCommit || prev.KeyID != keyID {
		return nil
	}
	old, ok := prev.Entry(e.Profile)
	if !ok || old.PlaintextSHA256 != e.PlaintextSHA256 {
		return nil
	}
	if data := prevFiles[old.File]; data != nil && SHA256Hex(data) == old.CiphertextSHA256 {
		return data
	}
	return nil
}
