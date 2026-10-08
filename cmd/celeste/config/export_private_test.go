package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Aikido 806869355: a session's saved command history and its JSON export
// never carry a set-key command's key, and exports are owner-only.
func TestSessionHistoryAndExportMaskSetKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s := &Session{ID: "s1"}
	s.SetCommandHistory([]string{"/config set-key sk-test-123", "hello"})
	// A session saved by an older version holds the raw line.
	s.Metadata["command_history"] = append(s.Metadata["command_history"].([]string), "/voice set-key el-test-456")
	for _, h := range s.GetCommandHistory() {
		if strings.Contains(h, "test-") {
			t.Errorf("history keeps a key: %q", h)
		}
	}
	out, err := NewExporter(s).ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "test-") {
		t.Errorf("JSON export carries a key: %s", out)
	}
	if runtime.GOOS == "windows" {
		return
	}
	path, err := NewExporter(s).SaveToFile(out, "json")
	if err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, path); got != 0o600 {
		t.Errorf("export mode = %v, want 0600", got)
	}
	if got := permOf(t, filepath.Dir(path)); got != 0o700 {
		t.Errorf("export dir mode = %v, want 0700", got)
	}
	_ = os.Remove(path)
}
