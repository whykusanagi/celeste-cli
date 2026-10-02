package selfupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/atomicfile"
)

// retryAfter is how long a failed upgrade of one tag waits (ruling 27).
const retryAfter = time.Hour

// Throttle remembers the last failed attempt, so an offline module build
// doesn't try on every run.
type Throttle struct {
	Path string // ~/.celeste/cache/selfupdate.json
	Now  func() time.Time
}

type attempt struct {
	Tag       string    `json:"tag"`
	Attempted time.Time `json:"attempted"`
}

// DefaultThrottle keeps its record in ~/.celeste/cache/selfupdate.json.
func DefaultThrottle() (*Throttle, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Throttle{Path: filepath.Join(home, ".celeste", "cache", "selfupdate.json"), Now: time.Now}, nil
}

// Due reports whether an upgrade to tag may be tried now. A missing or
// unreadable record, another tag, or a record from the future all mean yes.
func (t *Throttle) Due(tag string) bool {
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return true
	}
	var a attempt
	if json.Unmarshal(b, &a) != nil || a.Tag != tag {
		return true
	}
	since := t.Now().Sub(a.Attempted)
	return since < 0 || since >= retryAfter
}

// Record notes a failed attempt at tag now.
func (t *Throttle) Record(tag string) error {
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(attempt{Tag: tag, Attempted: t.Now().UTC()})
	if err != nil {
		return err
	}
	return atomicfile.Write(t.Path, b, 0o600)
}

// Clear forgets the record after a success.
func (t *Throttle) Clear() { _ = os.Remove(t.Path) }
