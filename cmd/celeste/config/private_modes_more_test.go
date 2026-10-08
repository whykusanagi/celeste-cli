package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
}

func privateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeOpen(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Review: a profile an older version left 0644 is tightened by Load even
// when it is never opened with LoadNamed.
func TestLoadTightensProfilesItDoesNotOpen(t *testing.T) {
	skipOnWindows(t)
	home := privateHome(t)
	p := filepath.Join(home, ".celeste", "config.unused.json")
	writeOpen(t, p, `{"api_key":"k"}`)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, p); got != 0o600 {
		t.Errorf("unused profile mode = %v, want 0600", got)
	}
}

// Review: slider.json and its directory are owner-only.
func TestSliderSaveIsOwnerOnly(t *testing.T) {
	skipOnWindows(t)
	home := privateHome(t)
	writeOpen(t, SliderPath(), `{}`)
	if err := os.Chmod(filepath.Join(home, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &SliderConfig{}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, SliderPath()); got != 0o600 {
		t.Errorf("slider.json mode = %v, want 0600", got)
	}
	if got := permOf(t, filepath.Dir(SliderPath())); got != 0o700 {
		t.Errorf("dir mode = %v, want 0700", got)
	}
}

// Review: analytics creates its directory owner-only.
func TestAnalyticsCreatesItsDirOwnerOnly(t *testing.T) {
	skipOnWindows(t)
	privateHome(t)
	if err := (&GlobalAnalytics{}).Save(); err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, filepath.Dir(GetAnalyticsPath())); got != 0o700 {
		t.Errorf("analytics dir mode = %v, want 0700", got)
	}
}

// Review: an export that lands on a leftover 0644 file of the same name
// leaves it owner-only.
func TestExportTightensAnExistingFile(t *testing.T) {
	skipOnWindows(t)
	privateHome(t)
	s := &Session{ID: "exp"}
	dir := GetExportDir()
	now := time.Now()
	var paths []string
	for i := 0; i < 3; i++ {
		ts := now.Add(time.Duration(i) * time.Second).Format("20060102_150405")
		p := filepath.Join(dir, "session_exp_"+ts+".md")
		writeOpen(t, p, "old")
		paths = append(paths, p)
	}
	got, err := NewExporter(s).SaveToFile("new", "md")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range paths {
		found = found || p == got
	}
	if !found {
		t.Skip("the export did not land on a leftover name (slow clock)")
	}
	if m := permOf(t, got); m != 0o600 {
		t.Errorf("export mode = %v, want 0600", m)
	}
}
