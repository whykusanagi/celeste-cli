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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync/atomic"
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

// Build returns a complete signed release of tag, as release.yml publishes
// it: the five archives, each holding bin under its contract name,
// checksums.txt, manifest.json, and both signatures by key's subkey.
func Build(t testing.TB, key *Key, tag string, bin []byte) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, p := range Platforms {
		if strings.HasSuffix(p.Archive, ".zip") {
			files[p.Archive] = Zip(t, map[string][]byte{p.Binary: bin})
		} else {
			files[p.Archive] = TarGz(t, map[string][]byte{p.Binary: bin})
		}
	}
	type artifact struct {
		Filename string `json:"filename"`
		SHA256   string `json:"sha256"`
	}
	var arts []artifact
	for _, p := range Platforms {
		h := sha256.Sum256(files[p.Archive])
		arts = append(arts, artifact{p.Archive, hex.EncodeToString(h[:])})
	}
	man, err := json.MarshalIndent(map[string]any{"version": strings.TrimPrefix(tag, "v"), "tag": tag, "artifacts": arts}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = man
	Resign(t, key, files)
	return files
}

// Resign recomputes checksums.txt from the archives in files and signs it
// and manifest.json again, after a test changed them.
func Resign(t testing.TB, key *Key, files map[string][]byte) {
	t.Helper()
	var sums strings.Builder
	for _, p := range Platforms {
		if a, ok := files[p.Archive]; ok {
			fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(a), p.Archive)
		}
	}
	files["checksums.txt"] = []byte(sums.String())
	files["checksums.txt.asc"] = key.Sign(t, files["checksums.txt"], time.Time{})
	files["manifest.json.asc"] = key.Sign(t, files["manifest.json"], time.Time{})
}

// Server is a fake GitHub release host over HTTPS. Like github.com, it
// redirects /releases/download/<tag>/<name> to an asset host (AssetBase, or
// itself) and /releases/latest to /releases/tag/<Latest>. Tests may edit
// Files, Latest and AssetBase before the first request.
type Server struct {
	*httptest.Server
	Files     map[string][]byte
	Latest    string
	AssetBase string
	requests  atomic.Int64
}

// Serve starts a Server for a release of tag; t.Cleanup closes it.
func Serve(t testing.TB, tag string, files map[string][]byte) *Server {
	t.Helper()
	s := &Server{Files: files, Latest: tag}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		http.Redirect(w, r, s.URL+"/releases/tag/"+s.Latest, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		base := s.AssetBase
		if base == "" {
			base = s.URL
		}
		http.Redirect(w, r, base+"/assets/"+path.Base(r.URL.Path), http.StatusFound)
	})
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		b, ok := s.Files[path.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	s.Server = httptest.NewTLSServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Host is the server's host:port, for an Updater's allowed Hosts.
func (s *Server) Host() string {
	u, _ := url.Parse(s.URL)
	return u.Host
}

// Requests counts every request the server has answered.
func (s *Server) Requests() int64 { return s.requests.Load() }
