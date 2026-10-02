// Package personacrypt seals and opens Celeste's persona profiles (W5
// rulings 2, 15, 16). The encryption is a gate and a legal signal, not
// secrecy: every official celeste binary carries the key.
package personacrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Format names the file layout and associated-data scheme; SOURCE.json
// records it.
const Format = "celeste-persona/v1 aes-256-gcm"

const magic = "CPv1"

// Overhead is what sealing adds to a plaintext: the magic, a 12-byte nonce
// and the 16-byte GCM tag.
const Overhead = len(magic) + 12 + 16

// Why a sealed persona can't be opened. These texts are fixed: no error
// from this package ever carries key material or the text a key was parsed
// from.
var (
	ErrNoKey        = errors.New("no persona key")
	ErrMalformedKey = errors.New("the persona key is not 64 hex characters")
	ErrWrongKey     = errors.New("the persona key does not match this persona")
	ErrDamaged      = errors.New("the encrypted persona is damaged")
)

// ParseKey decodes a 64-hex-character key; surrounding whitespace is
// ignored. A blank input is ErrNoKey, anything else unusable is
// ErrMalformedKey.
func ParseKey(hexKey string) ([]byte, error) {
	s := strings.TrimSpace(hexKey)
	if s == "" {
		return nil, ErrNoKey
	}
	if len(s) != 64 {
		return nil, ErrMalformedKey
	}
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, ErrMalformedKey // never wrap: hex errors quote the offending byte
	}
	return key, nil
}

// KeyID fingerprints a key for SOURCE.json, so a wrong key is told apart
// from damaged ciphertext. 64 bits of a domain-separated SHA-256 reveal
// nothing usable about a random 256-bit key.
func KeyID(key []byte) string {
	return SHA256Hex(append([]byte("celeste-persona key id/v1\x00"), key...))[:16]
}

// AAD binds a sealed file to its profile and the corpus commit it was built
// from, so a file can't be swapped into another profile or replayed under
// another pin.
func AAD(profile, sourceCommit string) []byte {
	return []byte("celeste-persona/v1\x00" + profile + "\x00" + sourceCommit)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrMalformedKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrMalformedKey
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext under key with a fresh random nonce:
// magic ‖ nonce ‖ ciphertext‖tag.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("persona nonce: %w", err)
	}
	out := append([]byte(magic), nonce...)
	return aead.Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts a sealed file. A wrong layout, wrong associated data, a
// changed byte and a wrong key are all ErrDamaged: GCM can't tell them
// apart. Callers compare KeyID first to report a wrong key.
func Open(key, sealed, aad []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < Overhead || !bytes.HasPrefix(sealed, []byte(magic)) {
		return nil, ErrDamaged
	}
	nonce := sealed[len(magic) : len(magic)+aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, sealed[len(magic)+aead.NonceSize():], aad)
	if err != nil {
		return nil, ErrDamaged
	}
	return plaintext, nil
}
