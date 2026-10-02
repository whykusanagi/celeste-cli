// Package selfupdatetest builds signed fake releases for the updater's
// tests (W5 rulings 26, 30): a generated OpenPGP key with a signing subkey,
// like the real release key, and the release archives. It does not import
// selfupdate, so its Platforms table is an independent statement of the
// asset contract.
package selfupdatetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"sort"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Platforms is the release asset contract (W5 ruling 30).
var Platforms = []struct{ GOOS, GOARCH, Archive, Binary string }{
	{"linux", "amd64", "celeste-linux-amd64.tar.gz", "celeste-linux-amd64"},
	{"linux", "arm64", "celeste-linux-arm64.tar.gz", "celeste-linux-arm64"},
	{"darwin", "amd64", "celeste-darwin-amd64.tar.gz", "celeste-darwin-amd64"},
	{"darwin", "arm64", "celeste-darwin-arm64.tar.gz", "celeste-darwin-arm64"},
	{"windows", "amd64", "celeste-windows-amd64.zip", "celeste-windows-amd64.exe"},
}

// Key is a generated release key: an Ed25519 primary with a signing subkey.
type Key struct {
	Entity *openpgp.Entity
	Public []byte // the armoured public key, standing in for ReleaseKey
}

// NewKey generates a key whose primary and subkey were created at created
// and expire after lifetime (0: never).
func NewKey(t testing.TB, created time.Time, lifetime time.Duration) *Key {
	t.Helper()
	cfg := &packet.Config{
		Algorithm:       packet.PubKeyAlgoEdDSA,
		Time:            func() time.Time { return created },
		KeyLifetimeSecs: uint32(lifetime / time.Second),
	}
	e, err := openpgp.NewEntity("celeste test release key", "", "release@example.invalid", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddSigningSubkey(cfg); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Serialize(w); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &Key{Entity: e, Public: buf.Bytes()}
}

// SubkeyID is the signing subkey's key id.
func (k *Key) SubkeyID() uint64 {
	for _, s := range k.Entity.Subkeys {
		if s.Sig != nil && s.Sig.FlagsValid && s.Sig.FlagSign {
			return s.PublicKey.KeyId
		}
	}
	panic("selfupdatetest: the key has no signing subkey")
}

// Sign returns an armoured detached signature over data by the signing
// subkey, as gpg --detach-sign --armor makes for a release, dated at (zero:
// now).
func (k *Key) Sign(t testing.TB, data []byte, at time.Time) []byte {
	t.Helper()
	if at.IsZero() {
		at = time.Now()
	}
	cfg := &packet.Config{SigningKeyId: k.SubkeyID(), Time: func() time.Time { return at }}
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, k.Entity, bytes.NewReader(data), cfg); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TarGz builds a .tar.gz of regular 0755 files, like release.yml's tar czf.
func TarGz(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range sortedNames(files) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Zip builds a .zip, like release.yml's zip.
func Zip(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedNames(files) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
