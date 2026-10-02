package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Review Focus 13: the Windows path runs on every OS through Updater.GOOS
// and the Rename seam.
func TestInstallWindowsRenamesTheRunningExe(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste.exe")
	u := testUpdater(s, key, exe)
	u.GOOS, u.GOARCH = "windows", "amd64"
	if _, err := u.Upgrade(context.Background(), tag); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, official) {
		t.Fatalf("exe = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "go install build" {
		t.Fatalf(".old = %q", b)
	}
}

func TestInstallWindowsRestoresOnFailure(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste.exe")
	u := testUpdater(s, key, exe)
	u.GOOS, u.GOARCH = "windows", "amd64"
	calls := 0
	u.Rename = func(oldpath, newpath string) error {
		calls++
		if calls == 2 { // putting the new file in place
			return errors.New("sharing violation")
		}
		return os.Rename(oldpath, newpath)
	}
	if _, err := u.Upgrade(context.Background(), tag); err == nil {
		t.Fatal("upgrade succeeded")
	}
	assertUnchanged(t, exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatalf(".old left behind: %v", err)
	}
}

// A previous .old that can't be deleted (still running) gets a unique name.
func TestInstallWindowsWithALockedOldFile(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste.exe")
	if err := os.MkdirAll(filepath.Join(exe+".old", "busy"), 0o755); err != nil { // os.Remove fails on it
		t.Fatal(err)
	}
	u := testUpdater(s, key, exe)
	u.GOOS, u.GOARCH = "windows", "amd64"
	if _, err := u.Upgrade(context.Background(), tag); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, official) {
		t.Fatalf("exe = %q", b)
	}
	matches, _ := filepath.Glob(exe + ".old-*")
	if len(matches) != 1 {
		t.Fatalf("old-* files: %v", matches)
	}
}

func TestCleanupOldRemovesLeftovers(t *testing.T) {
	exe := writeExe(t, "celeste.exe")
	dir := filepath.Dir(exe)
	for _, n := range []string{"celeste.exe.old", "celeste.exe.old-123", "other.exe.old"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	CleanupOld(exe)
	for n, want := range map[string]bool{"celeste.exe": true, "celeste.exe.old": false, "celeste.exe.old-123": false, "other.exe.old": true} {
		_, err := os.Stat(filepath.Join(dir, n))
		if got := err == nil; got != want {
			t.Errorf("%s exists = %v, want %v", n, got, want)
		}
	}
}

// A process killed between writing the temp file and renaming it leaves a
// full-size binary next to the executable; the next upgrade removes the
// ones older than an hour and leaves a concurrent upgrade's alone.
func TestUpgradeRemovesStaleTempFiles(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	dir := filepath.Dir(exe)
	stale := filepath.Join(dir, ".celeste.new-123")
	fresh := filepath.Join(dir, ".celeste.new-456")
	other := filepath.Join(dir, ".other.new-789")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range []string{stale, other} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := testUpdater(s, key, exe).Upgrade(context.Background(), tag); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stale temp file is still there: %v", err)
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(p), err)
		}
	}
}
