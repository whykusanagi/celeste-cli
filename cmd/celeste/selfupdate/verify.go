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
	ErrManifest     = errors.New("the signed release manifest is for another version")
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

// CheckManifestTag checks that a signed manifest.json describes tag, which
// binds the download to its version (ruling 26).
func CheckManifestTag(manifest []byte, tag string) error {
	var m struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return fmt.Errorf("%w (unreadable manifest)", ErrManifest)
	}
	if m.Tag != tag {
		return fmt.Errorf("%w: it is for %q, want %q", ErrManifest, m.Tag, tag)
	}
	return nil
}
