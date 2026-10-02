package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stateHome points HOME at a fresh directory with ~/.celeste in it.
func stateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0700); err != nil {
		t.Fatal(err)
	}
	return home
}

// assertSavedAtomically: save over an older 0644 file leaves exactly 0600
// (when want0600) and new content; then, with the directory read-only so
// the write cannot complete, a failed save leaves the old file intact.
func assertSavedAtomically(t *testing.T, path string, want0600 bool, save func() error) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == `{"old":true}` {
		t.Fatal("save did not replace the file")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0644)
		if want0600 {
			want = 0600
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("mode = %o, want %o", got, want)
		}
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return // a read-only directory does not stop the write there
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := save(); err == nil {
		t.Fatal("save into a read-only directory succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(data) {
		t.Fatalf("failed save changed the file: %q", after)
	}
}

func TestSaveSecretsIsAtomicAnd0600(t *testing.T) {
	home := stateHome(t)
	path := filepath.Join(home, ".celeste", "secrets.json")
	assertSavedAtomically(t, path, true, func() error { return SaveSecrets(&Config{APIKey: "k"}) })
}

func TestSaveSkillsConfigIsAtomicAnd0600(t *testing.T) {
	home := stateHome(t)
	path := filepath.Join(home, ".celeste", "skills.json")
	assertSavedAtomically(t, path, true, func() error { return SaveSkillsConfig(&Config{VeniceAPIKey: "v"}) })
}

func TestSessionSaveIsAtomicAnd0600(t *testing.T) {
	stateHome(t)
	m := NewSessionManager()
	s := &Session{ID: "s1"}
	path := filepath.Join(m.sessionsDir, "s1.json")
	assertSavedAtomically(t, path, true, func() error { return m.Save(s) })
}

func TestGlobalAnalyticsSaveIsAtomicAnd0600(t *testing.T) {
	stateHome(t)
	assertSavedAtomically(t, GetAnalyticsPath(), true, func() error { return NewGlobalAnalytics().Save() })
}

func TestPersistReconciledIsAtomicAndKeepsMode(t *testing.T) {
	home := stateHome(t)
	path := filepath.Join(home, ".celeste", "config.work.json")
	assertSavedAtomically(t, path, false, func() error {
		return persistReconciled(path, &Config{Model: "m"})
	})
}
