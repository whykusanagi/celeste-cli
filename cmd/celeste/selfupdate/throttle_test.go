package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestThrottle(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	th := &Throttle{Path: filepath.Join(t.TempDir(), "cache", "selfupdate.json"), Now: func() time.Time { return now }}
	if !th.Due("v2.0.0") {
		t.Fatal("no record: not due")
	}
	if err := th.Record("v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(th.Path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", fi.Mode())
		}
	}
	steps := []struct {
		at   time.Duration
		tag  string
		want bool
	}{
		{30 * time.Minute, "v2.0.0", false},
		{59 * time.Minute, "v2.0.0", false},
		{61 * time.Minute, "v2.0.0", true},
		{30 * time.Minute, "v2.0.1", true}, // another tag is not throttled
		{-time.Minute, "v2.0.0", true},     // a record from the future is stale
	}
	for _, s := range steps {
		at := now.Add(s.at)
		th.Now = func() time.Time { return at }
		if got := th.Due(s.tag); got != s.want {
			t.Errorf("Due(%s) at +%v = %v, want %v", s.tag, s.at, got, s.want)
		}
	}
	th.Clear()
	if !th.Due("v2.0.0") {
		t.Fatal("cleared: not due")
	}
	_ = os.WriteFile(th.Path, []byte("{garbage"), 0o600)
	if !th.Due("v2.0.0") {
		t.Fatal("an unreadable record must not block upgrades")
	}
}
