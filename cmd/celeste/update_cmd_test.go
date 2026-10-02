package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate/selfupdatetest"
)

// useFakeRelease serves a release of tag (also the latest) and points
// `celeste update` at it as a build of the given kind and current tag.
func useFakeRelease(t *testing.T, tag string, kind selfupdate.Kind, current string) (*selfupdatetest.Server, string) {
	t.Helper()
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	s := selfupdatetest.Serve(t, tag, selfupdatetest.Build(t, key, tag, []byte("official "+tag)))
	exe := standInExe(t)
	prevU, prevK := newUpdater, updateKind
	newUpdater = func() *selfupdate.Updater { return fakeUpdater(s, key, exe) }
	updateKind = func() (selfupdate.Kind, string) { return kind, current }
	t.Cleanup(func() { newUpdater, updateKind = prevU, prevK })
	return s, exe
}

func runUpdate(args ...string) (int, string, string) {
	var out, errBuf bytes.Buffer
	code := run(append([]string{"update"}, args...), &fakeRunner{}, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestUpdateRefusesASourceBuild(t *testing.T) {
	for _, kind := range []selfupdate.Kind{selfupdate.Source, selfupdate.Keyed} {
		s, exe := useFakeRelease(t, "v2.1.0", kind, "")
		code, _, stderr := runUpdate()
		if code != 1 || !strings.Contains(stderr, "built from source") {
			t.Fatalf("%v: exit %d, stderr %q", kind, code, stderr)
		}
		if s.Requests() != 0 {
			t.Fatalf("%v: a source build reached the network", kind)
		}
		if b, _ := os.ReadFile(exe); string(b) != "go install build" {
			t.Fatal("a source build was replaced")
		}
	}
}

func TestUpdateCheckReportsANewerRelease(t *testing.T) {
	_, exe := useFakeRelease(t, "v2.1.0", selfupdate.Official, "v2.0.0")
	code, stdout, _ := runUpdate("--check")
	if code != 0 || stdout != "celeste v2.1.0 is available (this is v2.0.0); run celeste update\n" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if b, _ := os.ReadFile(exe); string(b) != "go install build" {
		t.Fatal("--check installed something")
	}
}

func TestUpdateInstallsANewerRelease(t *testing.T) {
	_, exe := useFakeRelease(t, "v2.1.0", selfupdate.Official, "v2.0.0")
	code, stdout, stderr := runUpdate()
	if code != 0 || !strings.HasPrefix(stdout, "installed the official celeste v2.1.0 at ") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if b, _ := os.ReadFile(exe); string(b) != "official v2.1.0" {
		t.Fatalf("exe = %q", b)
	}
}

func TestUpdateNeverDowngrades(t *testing.T) {
	s, exe := useFakeRelease(t, "v2.0.0", selfupdate.Official, "v2.1.0")
	code, stdout, _ := runUpdate()
	if code != 0 || stdout != "celeste v2.1.0 is the latest release\n" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if s.Requests() != 1 { // only /releases/latest
		t.Fatalf("%d requests", s.Requests())
	}
	if b, _ := os.ReadFile(exe); string(b) != "go install build" {
		t.Fatal("downgraded")
	}
}

// A module build whose automatic upgrade was skipped gets the official
// build of its own tag even when it is the latest.
func TestUpdateFromAModuleBuildInstallsItsOwnTag(t *testing.T) {
	_, exe := useFakeRelease(t, "v2.0.0", selfupdate.Module, "v2.0.0")
	if code, _, stderr := runUpdate(); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if b, _ := os.ReadFile(exe); string(b) != "official v2.0.0" {
		t.Fatalf("exe = %q", b)
	}
}

func TestUpdateUsage(t *testing.T) {
	if code, _, stderr := runUpdate("now"); code != 2 || !strings.Contains(stderr, "Usage: celeste update [--check]") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
