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
)

// TrustStatus is whether a source may run without asking.
type TrustStatus int

const (
	Untrusted TrustStatus = iota // never approved
	Trusted                      // global, or approved with this exact hash
	Changed                      // approved before, but its hooks changed since
)

func (s TrustStatus) String() string {
	switch s {
	case Trusted:
		return "trusted"
	case Changed:
		return "changed"
	default:
		return "untrusted"
	}
}

type trustEntry struct {
	SHA256     string    `json:"sha256"`
	ApprovedAt time.Time `json:"approved_at"`
}

type trustFile struct {
	Version int                   `json:"version"`
	Hooks   map[string]trustEntry `json:"hooks"`
}

// TrustStore is ~/.celeste/trusted.json: approved repo hook sources, keyed
// by canonical path, pinned to the hash of the definitions approved.
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

func emptyTrust() trustFile { return trustFile{Version: 1, Hooks: map[string]trustEntry{}} }

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
	return f, nil
}

// Err reports why the store could not be read; nil when it is usable.
func (s *TrustStore) Err() error { return s.err }

// Status reports whether src may run without asking.
func (s *TrustStore) Status(src Source) TrustStatus {
	if src.Global() {
		return Trusted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data.Hooks[src.Path]
	switch {
	case !ok:
		return Untrusted
	case e.SHA256 == src.Hash:
		return Trusted
	default:
		return Changed
	}
}

// Approve records src's current hash. It re-reads the file first so another
// process's approvals survive, then replaces the file atomically. There is
// deliberately no lock file: two processes approving at the same instant can
// lose one approval, which only means that source is asked about again. It
// can never produce a spurious trust.
func (s *TrustStore) Approve(src Source) error {
	if src.Global() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return fmt.Errorf("not saving approval: %w", s.err)
	}
	fresh, err := readTrustFile(s.path)
	if err != nil {
		return fmt.Errorf("not saving approval: %w", err)
	}
	fresh.Version = 1
	fresh.Hooks[src.Path] = trustEntry{SHA256: src.Hash, ApprovedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path, append(data, '\n')); err != nil {
		return err
	}
	s.data = fresh
	return nil
}

// writeFileAtomic writes data to a 0600 temp file beside path, syncs it and
// renames it over path, so a crash never leaves a half-written trust file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".trusted-*.json") // created 0600
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
