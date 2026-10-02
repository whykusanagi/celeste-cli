package selfupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Repo is where official releases are published.
const Repo = "https://github.com/whykusanagi/celeste-cli"

// Hosts are the only hosts the updater talks to, over HTTPS only (ruling
// 26). GitHub redirects release downloads to release-assets.githubusercontent.com
// (objects.githubusercontent.com before 2025).
var Hosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// ErrHost is a request to anything but HTTPS on an allowed host.
var ErrHost = errors.New("the updater only talks HTTPS to GitHub")

// Updater downloads, verifies and installs official release binaries. New
// returns the production one; tests replace fields.
type Updater struct {
	Base      string            // the release repository, Repo
	Hosts     []string          // allowed hosts, Hosts
	Transport http.RoundTripper // nil: http.DefaultTransport's clone with ruling 26's timeouts
	PublicKey []byte            // the release key, ReleaseKey
	GOOS      string
	GOARCH    string
	Exe       string // the file to replace; "" = os.Executable() with symlinks resolved
	Rename    func(oldpath, newpath string) error
	Now       func() time.Time
}

// New returns an Updater for this binary and the official releases.
func New() *Updater {
	return &Updater{
		Base: Repo, Hosts: Hosts, PublicKey: ReleaseKey,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Rename: os.Rename, Now: time.Now,
	}
}

// hostGuard refuses every request that is not HTTPS to an allowed host. It
// sits in the transport, so it sees every redirect too.
type hostGuard struct {
	hosts []string
	next  http.RoundTripper
}

func (g *hostGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || !slices.Contains(g.hosts, r.URL.Host) {
		return nil, fmt.Errorf("%w (refused %s://%s)", ErrHost, r.URL.Scheme, r.URL.Host)
	}
	return g.next.RoundTrip(r)
}

func (u *Updater) client(follow bool) *http.Client {
	next := u.Transport
	if next == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		t.TLSHandshakeTimeout = 10 * time.Second
		t.ResponseHeaderTimeout = 20 * time.Second
		next = t
	}
	return &http.Client{
		Transport: &hostGuard{hosts: u.Hosts, next: next},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if !follow {
				return http.ErrUseLastResponse
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil // hostGuard checks the target
		},
	}
}

// fetch GETs url, following redirects, and returns at most max bytes.
func (u *Updater) fetch(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "celeste-updater")
	resp, err := u.client(true).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	name := path.Base(url)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", name, resp.Status)
	}
	if resp.ContentLength > max {
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}
	return b, nil
}

// LatestTag reads the newest release's tag from the redirect of
// <Base>/releases/latest (ruling 29): no API call, no rate limit.
func (u *Updater) LatestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.Base+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := u.client(false).Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	const marker = "/releases/tag/"
	i := strings.LastIndex(loc, marker)
	if resp.StatusCode/100 != 3 || i < 0 {
		return "", fmt.Errorf("latest release: unexpected answer %s", resp.Status)
	}
	tag := loc[i+len(marker):]
	if !ValidTag(tag) {
		return "", fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	return tag, nil
}

// Upgrade installs the official build of tag over the executable and
// returns the path it replaced. It replaces nothing unless every check of
// ruling 26 passed.
func (u *Updater) Upgrade(ctx context.Context, tag string) (string, error) {
	if !ValidTag(tag) {
		return "", fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	p, ok := Asset(u.GOOS, u.GOARCH)
	if !ok {
		return "", fmt.Errorf("%w (%s/%s)", ErrNoAsset, u.GOOS, u.GOARCH)
	}
	exe, err := u.exe()
	if err != nil {
		return "", err
	}
	dl := u.Base + "/releases/download/" + tag + "/"
	get := func(name string, max int64) ([]byte, error) { return u.fetch(ctx, dl+name, max) }
	bin, err := verifyRelease(get, u.PublicKey, tag, p)
	if err != nil {
		return "", err
	}
	if err := u.install(exe, bin); err != nil {
		return "", err
	}
	return exe, nil
}

// verifyRelease gets a release's files through get and returns p's binary
// only if, in this order: checksums.txt's signature verifies with pubKey,
// manifest.json's signature verifies, its tag is tag and it lists the same
// SHA-256 for p's archive as checksums.txt, and p's archive matches that
// checksum and holds the binary (ruling 26). Nothing
// later runs unless everything earlier passed.
func verifyRelease(get func(name string, max int64) ([]byte, error), pubKey []byte, tag string, p Platform) ([]byte, error) {
	sums, err := get("checksums.txt", limits.Checksums)
	if err != nil {
		return nil, err
	}
	sumsSig, err := get("checksums.txt.asc", limits.Signature)
	if err != nil {
		return nil, err
	}
	if err := VerifySignature(pubKey, sums, sumsSig); err != nil {
		return nil, fmt.Errorf("checksums.txt: %w", err)
	}
	man, err := get("manifest.json", limits.Manifest)
	if err != nil {
		return nil, err
	}
	manSig, err := get("manifest.json.asc", limits.Signature)
	if err != nil {
		return nil, err
	}
	if err := VerifySignature(pubKey, man, manSig); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	listed, err := CheckManifest(man, tag, p.Archive)
	if err != nil {
		return nil, err
	}
	want, err := FindChecksum(sums, p.Archive)
	if err != nil {
		return nil, err
	}
	if want != listed {
		return nil, fmt.Errorf("%w: checksums.txt and the %s manifest disagree on %s", ErrChecksum, tag, p.Archive)
	}
	data, err := get(p.Archive, limits.Archive)
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(data) != want {
		return nil, fmt.Errorf("%w: %s", ErrChecksum, p.Archive)
	}
	return Extract(p.Archive, data, p.Binary)
}

func (u *Updater) exe() (string, error) {
	p := u.Exe
	if p == "" {
		var err error
		if p, err = os.Executable(); err != nil {
			return "", fmt.Errorf("find this executable: %w", err)
		}
	}
	return filepath.EvalSymlinks(p)
}
