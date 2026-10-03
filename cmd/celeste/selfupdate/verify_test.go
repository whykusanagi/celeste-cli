package selfupdate

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/selfupdate/selfupdatetest"
)

var signedData = []byte("a6cc94fd  celeste-darwin-amd64.tar.gz\n")

func issuer(t *testing.T, armoredSig []byte) uint64 {
	t.Helper()
	block, err := armor.Decode(bytes.NewReader(armoredSig))
	if err != nil {
		t.Fatal(err)
	}
	p, err := packet.Read(block.Body)
	if err != nil {
		t.Fatal(err)
	}
	return *p.(*packet.Signature).IssuerKeyId
}

// Ruling 26: releases are signed by the signing subkey, not the primary.
func TestVerifyAcceptsTheSigningSubkey(t *testing.T) {
	k := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	sig := k.Sign(t, signedData, time.Time{})
	if got := issuer(t, sig); got != k.SubkeyID() || got == k.Entity.PrimaryKey.KeyId {
		t.Fatalf("test signature issuer %X is not the signing subkey %X", got, k.SubkeyID())
	}
	if err := VerifySignature(k.Public, signedData, sig); err != nil {
		t.Fatal(err)
	}
}

// Review Focus 9.
func TestVerifyRejectsATamperedFile(t *testing.T) {
	k := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	sig := k.Sign(t, signedData, time.Time{})
	tampered := append(bytes.Clone(signedData), '\n')
	if err := VerifySignature(k.Public, tampered, sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestVerifyRejectsAnotherKey(t *testing.T) {
	k := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	other := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	if err := VerifySignature(k.Public, signedData, other.Sign(t, signedData, time.Time{})); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	k := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	for name, sig := range map[string][]byte{
		"empty":                    nil,
		"not armour":               []byte("not a signature"),
		"a public key, not a sig":  k.Public,
		"truncated armoured block": k.Sign(t, signedData, time.Time{})[:60],
	} {
		if err := VerifySignature(k.Public, signedData, sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v, want ErrBadSignature", name, err)
		}
	}
	if err := VerifySignature([]byte("no key"), signedData, k.Sign(t, signedData, time.Time{})); !errors.Is(err, ErrBadSignature) {
		t.Errorf("unreadable key: err = %v, want ErrBadSignature", err)
	}
}

// Ruling 26: a release signed while the subkey was valid stays verifiable
// after the subkey expires (the real one expires 2027-12-07).
func TestVerifyAcceptsASignatureMadeBeforeTheKeyExpired(t *testing.T) {
	created := time.Now().Add(-48 * time.Hour)
	k := selfupdatetest.NewKey(t, created, time.Hour) // expired 47 hours ago
	sig := k.Sign(t, signedData, created.Add(30*time.Minute))
	if err := VerifySignature(k.Public, signedData, sig); err != nil {
		t.Fatalf("a signature made inside the key's lifetime failed: %v", err)
	}
}

// The real key, the real v1.16.0 signatures (public release assets).
func TestVerifyARealReleaseSignature(t *testing.T) {
	for _, name := range []string{"checksums.txt", "manifest.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "v1.16.0", name))
		if err != nil {
			t.Fatal(err)
		}
		sig, err := os.ReadFile(filepath.Join("testdata", "v1.16.0", name+".asc"))
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifySignature(ReleaseKey, data, sig); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := VerifySignature(ReleaseKey, append(data, ' '), sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s tampered: err = %v, want ErrBadSignature", name, err)
		}
	}
}

func TestEmbeddedReleaseKeyMatchesTheRepositoryKey(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "..", "whykusanagi.asc"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(root, ReleaseKey) {
		t.Fatal("cmd/celeste/selfupdate/release_key.asc differs from whykusanagi.asc: copy it again (cp whykusanagi.asc cmd/celeste/selfupdate/release_key.asc)")
	}
}

func TestEmbeddedReleaseKeyHasTheSigningSubkey(t *testing.T) {
	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(ReleaseKey))
	if err != nil || len(ring) != 1 {
		t.Fatalf("ring %d entities, err %v", len(ring), err)
	}
	if got := fmt.Sprintf("%X", ring[0].PrimaryKey.Fingerprint); got != "940490EF09DA31322BF7FD83875849AB1D541C55" {
		t.Fatalf("primary fingerprint %s", got)
	}
	for _, s := range ring[0].Subkeys {
		if fmt.Sprintf("%X", s.PublicKey.Fingerprint) == "F4C254F6EE5D7F086C921DEBA6BB54DDC70EE8FB" {
			if s.Sig == nil || !s.Sig.FlagsValid || !s.Sig.FlagSign {
				t.Fatal("subkey F4C254… is not marked for signing")
			}
			return
		}
	}
	t.Fatal("the signing subkey F4C254F6EE5D7F086C921DEBA6BB54DDC70EE8FB is missing")
}

func TestFindChecksum(t *testing.T) {
	sums, err := os.ReadFile(filepath.Join("testdata", "v1.16.0", "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := FindChecksum(sums, "celeste-darwin-arm64.tar.gz")
	if err != nil || fmt.Sprintf("%x", got) != "846ab8b14a3d2c29534d7033201f5be999a82fdc5e0d9286059949528918fb7f" {
		t.Fatalf("got %x, %v", got, err)
	}
	if _, err := FindChecksum([]byte(fmt.Sprintf("%064x *celeste-linux-amd64.tar.gz\n", 1)), "celeste-linux-amd64.tar.gz"); err != nil {
		t.Fatalf("binary-mode line: %v", err)
	}
	h := fmt.Sprintf("%064x", 7)
	for name, file := range map[string]string{
		"not listed":   h + "  celeste-other.tar.gz\n",
		"listed twice": h + "  celeste-linux-amd64.tar.gz\n" + h + "  celeste-linux-amd64.tar.gz\n",
		"short hash":   "abcd  celeste-linux-amd64.tar.gz\n",
		"not hex":      fmt.Sprintf("%064s  celeste-linux-amd64.tar.gz\n", "z"),
		"prefix only":  h + "  celeste-linux-amd64.tar.gz.old\n",
	} {
		if _, err := FindChecksum([]byte(file), "celeste-linux-amd64.tar.gz"); !errors.Is(err, ErrChecksum) {
			t.Errorf("%s: err = %v, want ErrChecksum", name, err)
		}
	}
}

func TestCheckManifest(t *testing.T) {
	m, err := os.ReadFile(filepath.Join("testdata", "v1.16.0", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	const archive = "celeste-darwin-arm64.tar.gz"
	got, err := CheckManifest(m, "v1.16.0", archive)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got[:]) != "846ab8b14a3d2c29534d7033201f5be999a82fdc5e0d9286059949528918fb7f" {
		t.Fatalf("sha256 = %x", got)
	}
	sums, err := os.ReadFile(filepath.Join("testdata", "v1.16.0", "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range Platforms {
		fromManifest, err := CheckManifest(m, "v1.16.0", p.Archive)
		if err != nil {
			t.Fatalf("%s: %v", p.Archive, err)
		}
		if fromSums, err := FindChecksum(sums, p.Archive); err != nil || fromSums != fromManifest {
			t.Fatalf("%s: the real manifest and checksums disagree (%v)", p.Archive, err)
		}
	}
	if _, err := CheckManifest(m, "v2.0.0", archive); !errors.Is(err, ErrManifest) {
		t.Fatalf("another tag: err = %v, want ErrManifest", err)
	}
	h := strings.Repeat("ab", 32)
	for name, man := range map[string]string{
		"garbage":      `{`,
		"no artifacts": `{"tag": "v1.16.0"}`,
		"not listed":   `{"tag": "v1.16.0", "artifacts": [{"filename": "celeste-linux-amd64.tar.gz", "sha256": "` + h + `"}]}`,
		"listed twice": `{"tag": "v1.16.0", "artifacts": [{"filename": "` + archive + `", "sha256": "` + h + `"}, {"filename": "` + archive + `", "sha256": "` + h + `"}]}`,
		"bad hex":      `{"tag": "v1.16.0", "artifacts": [{"filename": "` + archive + `", "sha256": "zz"}]}`,
		"short sha256": `{"tag": "v1.16.0", "artifacts": [{"filename": "` + archive + `", "sha256": "abab"}]}`,
		"wrong type":   `{"tag": "v1.16.0", "artifacts": {"filename": "` + archive + `"}}`,
	} {
		if _, err := CheckManifest([]byte(man), "v1.16.0", archive); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: err = %v, want ErrManifest", name, err)
		}
	}
}
