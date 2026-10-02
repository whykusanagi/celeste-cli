package selfupdate

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// ReleaseKey is celeste's release signing key: a byte-identical copy of the
// repository's whykusanagi.asc (TestEmbeddedReleaseKeyMatchesTheRepositoryKey).
// Primary 940490EF09DA31322BF7FD83875849AB1D541C55; releases are signed by
// its subkey F4C254F6EE5D7F086C921DEBA6BB54DDC70EE8FB.
//
//go:embed release_key.asc
var ReleaseKey []byte

var (
	ErrBadSignature = errors.New("the release signature does not verify with celeste's release key")
	ErrChecksum     = errors.New("the download does not match the signed checksums")
	ErrManifest     = errors.New("the signed release manifest does not describe this download")
)

// VerifySignature checks an ASCII-armoured detached signature over signed
// with the armoured public key pubKey (ruling 26). It verifies as of the
// time the signature was made, so a release stays verifiable after the
// signing subkey expires; only the key's holder can date a signature. Every
// failure is ErrBadSignature: there is no outcome but nil or an error.
func VerifySignature(pubKey, signed, armoredSig []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubKey))
	if err != nil || len(keyring) == 0 {
		return fmt.Errorf("%w (the release key is unreadable)", ErrBadSignature)
	}
	made, err := signatureTime(armoredSig)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrBadSignature, err)
	}
	cfg := &packet.Config{Time: func() time.Time { return made }}
	if _, err := openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(signed), bytes.NewReader(armoredSig), cfg); err != nil {
		return fmt.Errorf("%w (%v)", ErrBadSignature, err)
	}
	return nil
}

// signatureTime reads the creation time of the first signature packet.
func signatureTime(armoredSig []byte) (time.Time, error) {
	block, err := armor.Decode(bytes.NewReader(armoredSig))
	if err != nil {
		return time.Time{}, errors.New("not an armoured signature")
	}
	if block.Type != openpgp.SignatureType {
		return time.Time{}, fmt.Errorf("armour type %q, want %q", block.Type, openpgp.SignatureType)
	}
	p, err := packet.Read(block.Body)
	if err != nil {
		return time.Time{}, errors.New("unreadable signature packet")
	}
	sig, ok := p.(*packet.Signature)
	if !ok {
		return time.Time{}, errors.New("not a signature packet")
	}
	return sig.CreationTime, nil
}

// FindChecksum returns the SHA-256 that a sha256sum-format file lists for
// name (text "<hex>  <name>" or binary "<hex> *<name>" lines). Call it only
// on a file whose signature verified. A name listed twice is an error.
func FindChecksum(checksums []byte, name string) ([32]byte, error) {
	var sum [32]byte
	found := false
	sc := bufio.NewScanner(bytes.NewReader(checksums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if found {
			return sum, fmt.Errorf("%w: %s is listed twice", ErrChecksum, name)
		}
		b, err := hex.DecodeString(fields[0])
		if err != nil || len(b) != len(sum) {
			return sum, fmt.Errorf("%w: malformed line for %s", ErrChecksum, name)
		}
		copy(sum[:], b)
		found = true
	}
	if !found {
		return sum, fmt.Errorf("%w: %s is not listed", ErrChecksum, name)
	}
	return sum, nil
}

// CheckManifest checks that a signed manifest.json describes tag and
// returns the SHA-256 it lists for archive. The caller requires that sum to
// equal checksums.txt's, which binds the download to its version (ruling
// 26): checksums.txt names no version, so an older release's signed
// checksums and archive copied into this release would otherwise pass. A
// missing, malformed or repeated entry is ErrManifest. Call it only on a
// manifest whose signature verified.
func CheckManifest(manifest []byte, tag, archive string) ([32]byte, error) {
	var sum [32]byte
	var m struct {
		Tag       string `json:"tag"`
		Artifacts []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return sum, fmt.Errorf("%w (unreadable manifest)", ErrManifest)
	}
	if m.Tag != tag {
		return sum, fmt.Errorf("%w: it is for %q, want %q", ErrManifest, m.Tag, tag)
	}
	found := false
	for _, a := range m.Artifacts {
		if a.Filename != archive {
			continue
		}
		if found {
			return sum, fmt.Errorf("%w: it lists %s twice", ErrManifest, archive)
		}
		b, err := hex.DecodeString(a.SHA256)
		if err != nil || len(b) != len(sum) {
			return sum, fmt.Errorf("%w: malformed sha256 for %s", ErrManifest, archive)
		}
		copy(sum[:], b)
		found = true
	}
	if !found {
		return sum, fmt.Errorf("%w: it does not list %s", ErrManifest, archive)
	}
	return sum, nil
}
