package personacrypt

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

var (
	pinA = strings.Repeat("a", 40)
	pinB = strings.Repeat("b", 40)
)

func TestPersonacryptRoundTrip(t *testing.T) {
	key, aad := testKey(7), AAD("full", pinA)
	sealed, err := Seal(key, []byte("plain persona"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != len("plain persona")+Overhead {
		t.Fatalf("sealed %d bytes, want %d", len(sealed), len("plain persona")+Overhead)
	}
	if bytes.Contains(sealed, []byte("plain persona")) {
		t.Fatal("the plaintext is visible in the sealed bytes")
	}
	got, err := Open(key, sealed, aad)
	if err != nil || string(got) != "plain persona" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	again, _ := Seal(key, []byte("plain persona"), aad)
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals of one plaintext are identical: the nonce was reused")
	}
}

// Every way a sealed file can be wrong is ErrDamaged (ruling 16).
func TestPersonacryptOpenFailsClosed(t *testing.T) {
	key, aad := testKey(7), AAD("full", pinA)
	sealed, _ := Seal(key, []byte("plain persona"), aad)
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	for name, tc := range map[string]struct{ key, sealed, aad []byte }{
		"wrong key":     {testKey(8), sealed, aad},
		"flipped bit":   {key, flipped, aad},
		"other profile": {key, sealed, AAD("spine", pinA)},
		"other pin":     {key, sealed, AAD("full", pinB)},
		"truncated":     {key, sealed[:Overhead-1], aad},
		"no magic":      {key, append([]byte("XXXX"), sealed[4:]...), aad},
	} {
		if _, err := Open(tc.key, tc.sealed, tc.aad); !errors.Is(err, ErrDamaged) {
			t.Errorf("%s: err = %v, want ErrDamaged", name, err)
		}
	}
	if _, err := Seal(testKey(7)[:16], []byte("x"), aad); !errors.Is(err, ErrMalformedKey) {
		t.Errorf("a 16-byte key: err = %v, want ErrMalformedKey", err)
	}
}

func TestPersonacryptParseKey(t *testing.T) {
	good := strings.Repeat("ab", 32)
	if k, err := ParseKey("  " + good + "\n"); err != nil || hex.EncodeToString(k) != good {
		t.Fatalf("ParseKey(good) = %x, %v", k, err)
	}
	if _, err := ParseKey(" \n"); !errors.Is(err, ErrNoKey) {
		t.Errorf("blank: %v, want ErrNoKey", err)
	}
	for _, bad := range []string{good[:63], good + "a", strings.Repeat("zz", 32)} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrMalformedKey) {
			t.Errorf("%d chars: %v, want ErrMalformedKey", len(bad), err)
		}
	}
}

// No error text carries the key or the text it was parsed from (ruling 15).
func TestPersonacryptParseKeyErrorsNeverQuoteTheInput(t *testing.T) {
	for _, input := range []string{strings.Repeat("c0ffee", 10) + "zz", strings.Repeat("c0ffee", 10) + "zzzz"} {
		if _, err := ParseKey(input); err == nil || strings.Contains(err.Error(), "c0ffee") {
			t.Fatalf("ParseKey error %q quotes its input", err)
		}
	}
	key := testKey(9)
	if _, err := Open(key, []byte("CPv1short"), nil); err == nil || strings.Contains(err.Error(), hex.EncodeToString(key)) {
		t.Fatalf("Open error %q", err)
	}
}

func TestPersonacryptKeyID(t *testing.T) {
	a, b := KeyID(testKey(1)), KeyID(testKey(2))
	if len(a) != 16 || a == b || a != KeyID(testKey(1)) {
		t.Fatalf("KeyID: %q, %q", a, b)
	}
}

func plainSet(full string) map[string][]byte {
	out := map[string][]byte{}
	for _, p := range Profiles {
		text := "prompt for " + p
		if p == "full" {
			text = full
		}
		out[p] = []byte(`{"profile":"` + p + `","system_prompt":"` + text + `"}`)
	}
	return out
}

// SOURCE.json describes every file exactly; unchanged plaintext keeps its
// ciphertext; a new key re-seals (ruling 16).
func TestPersonacryptSealSetRecordsAndReuses(t *testing.T) {
	key := testKey(3)
	src, files, err := SealSet(key, pinA, pinB, plainSet("v1"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if src.Format != Format || src.KeyID != KeyID(key) || len(src.Profiles) != len(Profiles) {
		t.Fatalf("source = %+v", src)
	}
	for _, e := range src.Profiles {
		data := files[e.File]
		if e.File != FileName(e.Profile) || len(data) != e.CiphertextBytes || SHA256Hex(data) != e.CiphertextSHA256 || e.CiphertextBytes != e.PlaintextBytes+Overhead {
			t.Fatalf("entry %+v does not describe its file", e)
		}
		pt, err := Open(key, data, AAD(e.Profile, pinA))
		if err != nil || SHA256Hex(pt) != e.PlaintextSHA256 {
			t.Fatalf("%s does not open to its recorded plaintext", e.Profile)
		}
	}
	if parsed, err := ParseSource(src.JSON()); err != nil || parsed.KeyID != src.KeyID {
		t.Fatalf("ParseSource(JSON()) = %+v, %v", parsed, err)
	}
	_, files2, err := SealSet(key, pinA, pinB, plainSet("v2"), &src, files)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(files2[FileName("full")], files[FileName("full")]) {
		t.Error("a changed full kept its old ciphertext")
	}
	if !bytes.Equal(files2[FileName("spine")], files[FileName("spine")]) {
		t.Error("an unchanged spine was re-sealed")
	}
	_, files3, _ := SealSet(testKey(4), pinA, pinB, plainSet("v1"), &src, files)
	if bytes.Equal(files3[FileName("spine")], files[FileName("spine")]) {
		t.Error("a new key reused old ciphertext")
	}
}

func TestPersonacryptSealSetRejectsUnbuiltProfiles(t *testing.T) {
	plain := plainSet("v1")
	plain["lite"] = []byte(`{"version":"3.1.0","system_prompt_preamble":"x"}`)
	if _, _, err := SealSet(testKey(3), pinA, pinB, plain, nil, nil); err == nil {
		t.Fatal("an unbuilt corpus template was sealed")
	}
	delete(plain, "lite")
	if _, _, err := SealSet(testKey(3), pinA, pinB, plain, nil, nil); err == nil {
		t.Fatal("a set missing lite was sealed")
	}
}

func TestPersonacryptParseSourceRejectsBadRecords(t *testing.T) {
	src, _, err := SealSet(testKey(3), pinA, pinB, plainSet("v1"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Source){
		"format":     func(s *Source) { s.Format = "v0" },
		"short pin":  func(s *Source) { s.CoreCommit = "abc1234" },
		"key_id":     func(s *Source) { s.KeyID = "xyz" },
		"order":      func(s *Source) { s.Profiles[0], s.Profiles[1] = s.Profiles[1], s.Profiles[0] },
		"file name":  func(s *Source) { s.Profiles[0].File = "../celeste_full.enc" },
		"size claim": func(s *Source) { s.Profiles[2].PlaintextBytes++ },
	} {
		s := src
		s.Profiles = append([]Entry(nil), src.Profiles...)
		mutate(&s)
		if _, err := ParseSource(s.JSON()); err == nil {
			t.Errorf("%s: a bad SOURCE.json parsed", name)
		}
	}
}
