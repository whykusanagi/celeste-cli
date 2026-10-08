package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
)

// TrustStatus is whether a source may run without asking.
type TrustStatus int

const (
	Untrusted       TrustStatus = iota // never approved or declined
	Trusted                            // global, or approved with this exact hash
	Changed                            // approved before, but its hooks changed since
	Declined                           // declined with this exact hash: never runs, never asked again
	DeclinedChanged                    // declined before, but it changed since: asked again
)

func (s TrustStatus) String() string {
	switch s {
	case Trusted:
		return "trusted"
	case Changed:
		return "changed"
	case Declined:
		return "declined"
	case DeclinedChanged:
		return "changed since declined"
	default:
		return "untrusted"
	}
}

type trustEntry struct {
	SHA256     string    `json:"sha256"`
	ApprovedAt time.Time `json:"approved_at"`
}

// declineEntry is a source the person said no to, pinned to the hash they
// saw (#411).
type declineEntry struct {
	SHA256     string    `json:"sha256"`
	DeclinedAt time.Time `json:"declined_at"`
}

// trustFile is the store's JSON. Declines live in their own map, never in
// "hooks": an older celeste reads only "hooks", and would take an entry
// there with a matching hash for an approval.
type trustFile struct {
	Version  int                     `json:"version"`
	Hooks    map[string]trustEntry   `json:"hooks"`
	Declined map[string]declineEntry `json:"declined,omitempty"`
}

// TrustStore is ~/.celeste/trusted.json: approved and declined repo hook
// sources, keyed by canonical path, pinned to the hash of the definitions
// the person saw.
type TrustStore struct {
	path string
	mu   sync.Mutex
	data trustFile
	err  error // the file exists but can't be read; approvals are refused
}

// TrustPath is where approvals are stored.
func TrustPath(home string) string { return filepath.Join(home, ".celeste", "trusted.json") }

// LoadTrust reads the store. It never returns nil; check Err.
func LoadTrust(home string) *TrustStore {
	s := &TrustStore{path: TrustPath(home)}
	s.data, s.err = readTrustFile(s.path)
	return s
}

func emptyTrust() trustFile {
	return trustFile{Version: 1, Hooks: map[string]trustEntry{}, Declined: map[string]declineEntry{}}
}

func readTrustFile(path string) (trustFile, error) {
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyTrust(), nil
	}
	if err != nil {
		return emptyTrust(), err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, maxSourceBytes+1))
	if err != nil {
		return emptyTrust(), err
	}
	if len(data) > maxSourceBytes {
		return emptyTrust(), fmt.Errorf("%s is larger than 1 MiB", path)
	}
	f := emptyTrust()
	if err := json.Unmarshal(data, &f); err != nil {
		return emptyTrust(), fmt.Errorf("%s is corrupt: %w", path, err)
	}
	if f.Hooks == nil {
		f.Hooks = map[string]trustEntry{}
	}
	if f.Declined == nil {
		f.Declined = map[string]declineEntry{}
	}
	return f, nil
}

// Err reports why the store could not be read; nil when it is usable.
func (s *TrustStore) Err() error { return s.err }

// Status reports whether src may run without asking. A decline of this
// exact hash wins over any approval.
func (s *TrustStore) Status(src Source) TrustStatus {
	if src.Global() {
		return Trusted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, declined := s.data.Declined[src.Path]
	e, approved := s.data.Hooks[src.Path]
	switch {
	case declined && d.SHA256 == src.Hash:
		return Declined
	case approved && e.SHA256 == src.Hash:
		return Trusted
	case approved:
		return Changed
	case declined:
		return DeclinedChanged
	}
	return Untrusted
}

// Approve records src's current hash, replacing any decline. It re-reads
// the file first so another process's decisions survive, then replaces the
// file atomically. There is deliberately no lock file: two processes
// deciding at the same instant can lose one decision, which only means that
// source is asked about again. It can never produce a spurious trust.
func (s *TrustStore) Approve(src Source) error {
	if src.Global() {
		return nil
	}
	return s.update("approval", func(f *trustFile) bool {
		delete(f.Declined, src.Path)
		f.Hooks[src.Path] = trustEntry{SHA256: src.Hash, ApprovedAt: time.Now().UTC()}
		return true
	})
}

// Decline records that the person said no to src's current hash, replacing
// any approval: it never runs and is not asked about again until it
// changes (#411). Like Approve, it re-reads the file first.
func (s *TrustStore) Decline(src Source) error {
	if src.Global() {
		return nil
	}
	return s.update("decline", func(f *trustFile) bool {
		delete(f.Hooks, src.Path)
		f.Declined[src.Path] = declineEntry{SHA256: src.Hash, DeclinedAt: time.Now().UTC()}
		return true
	})
}

// Forget removes any approval or decline stored under key (a Source's
// Path), so the source is asked about again. forgot is false when nothing
// was stored; the file is then left untouched.
func (s *TrustStore) Forget(key string) (forgot bool, err error) {
	err = s.update("change", func(f *trustFile) bool {
		_, a := f.Hooks[key]
		_, d := f.Declined[key]
		delete(f.Hooks, key)
		delete(f.Declined, key)
		forgot = a || d
		return forgot
	})
	return forgot, err
}

// update applies change to a fresh read of the file and writes it back
// when change reports a modification. what names the decision in errors.
func (s *TrustStore) update(what string, change func(*trustFile) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return fmt.Errorf("not saving %s: %w", what, s.err)
	}
	fresh, err := readTrustFile(s.path)
	if err != nil {
		return fmt.Errorf("not saving %s: %w", what, err)
	}
	if !change(&fresh) {
		s.data = fresh
		return nil
	}
	fresh.Version = 1
	data, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := atomicfile.Write(s.path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	s.data = fresh
	return nil
}
