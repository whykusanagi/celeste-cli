package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// validID limits tool-call IDs to characters that are safe in a file name;
// provider IDs (toolu_…, call_…) fit.
var validID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// Store keeps pruned tool-result bodies on disk so recall_tool_result can
// return them.
type Store struct {
	Dir string
}

// DefaultStore is ~/.celeste/tool-results/pruned.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Store{Dir: filepath.Join(home, ".celeste", "tool-results", "pruned")}, nil
}

func (s *Store) path(id string) (string, error) {
	if !validID.MatchString(id) || id == "." || id == ".." {
		return "", fmt.Errorf("invalid tool call id %q", id)
	}
	return filepath.Join(s.Dir, id+".txt"), nil
}

// Save writes a pruned body. The file is private: tool output can hold
// secrets.
func (s *Store) Save(id, body string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(body), 0o600)
}

// Load reads a pruned body back.
func (s *Store) Load(id string) (string, error) {
	p, err := s.path(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no pruned tool result with id %q", id)
		}
		return "", err
	}
	return string(b), nil
}

// Commit spills every edit's original body and returns the edits whose body
// was saved. An edit whose spill fails is dropped, so nothing is ever
// replaced by a pointer to a file that doesn't exist.
func (s *Store) Commit(edits []Edit) []Edit {
	kept := edits[:0:0]
	for _, e := range edits {
		if err := s.Save(e.ToolCallID, e.Original); err == nil {
			kept = append(kept, e)
		}
	}
	return kept
}
