package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate/selfupdatetest"
)

const upTag = "v2.0.0"

var officialBin = []byte("official celeste v2.0.0")

// fakeUpdater points an Updater at a fake release and a stand-in executable.
func fakeUpdater(s *selfupdatetest.Server, key *selfupdatetest.Key, exe string) *selfupdate.Updater {
	u := selfupdate.New()
	u.Base = s.URL
	u.Hosts = []string{s.Host()}
	u.Transport = s.Client().Transport
	u.PublicKey = key.Public
	u.GOOS, u.GOARCH = "linux", "amd64"
	u.Exe = exe
	return u
}

func standInExe(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "celeste")
	if err := os.WriteFile(exe, []byte("go install build"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

type reexecCall struct {
	exe       string
	argv, env []string
}

type hookRig struct {
	hook   *upgradeHook
	server *selfupdatetest.Server
	key    *selfupdatetest.Key
	exe    string
	stderr *bytes.Buffer
	execs  *[]reexecCall
	env    map[string]string
}

// newRig builds a hook for a module build of upTag against a fake release.
// HOME is a temp dir, so the throttle never touches the developer's.
func newRig(t *testing.T, files func(*selfupdatetest.Key) map[string][]byte) *hookRig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	f := selfupdatetest.Build(t, key, upTag, officialBin)
	if files != nil {
		f = files(key)
	}
	s := selfupdatetest.Serve(t, upTag, f)
	exe := standInExe(t)
	r := &hookRig{server: s, key: key, exe: exe, stderr: &bytes.Buffer{}, execs: &[]reexecCall{}, env: map[string]string{}}
	th, err := selfupdate.DefaultThrottle()
	if err != nil {
		t.Fatal(err)
	}
	r.hook = &upgradeHook{
		kind:     selfupdate.Module,
		tag:      upTag,
		getenv:   func(k string) string { return r.env[k] },
		updater:  fakeUpdater(s, key, exe),
		throttle: th,
		reexec: func(exe string, argv, env []string) error {
			*r.execs = append(*r.execs, reexecCall{exe, argv, env})
			return nil
		},
		unsetenv: func(k string) error { delete(r.env, k); return nil },
		environ:  func() []string { return []string{"PATH=/usr/bin"} },
		stderr:   r.stderr,
		timeout:  30 * time.Second,
	}
	return r
}

func (r *hookRig) exeBytes(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Review Focus 11: the same invocation continues as the official binary.
func TestAutoUpgradeInstallsAndReexecs(t *testing.T) {
	r := newRig(t, nil)
	argv := []string{"celeste", "-config", "work", "message", "hi"}
	r.hook.beforeRun(argv)
	if got := r.exeBytes(t); got != string(officialBin) {
		t.Fatalf("exe = %q", got)
	}
	if len(*r.execs) != 1 {
		t.Fatalf("%d re-execs", len(*r.execs))
	}
	c := (*r.execs)[0]
	if c.exe != r.exe || strings.Join(c.argv, " ") != strings.Join(argv, " ") {
		t.Fatalf("re-exec %s %v", c.exe, c.argv)
	}
	if !contains(c.env, selfupdatedEnv+"="+upTag) || !contains(c.env, "PATH=/usr/bin") {
		t.Fatalf("env %v", c.env)
	}
	out := r.stderr.String()
	if !strings.Contains(out, "installing the official v2.0.0 build for linux/amd64") || !strings.Contains(out, "installed the official v2.0.0 build") {
		t.Fatalf("stderr = %q", out)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Review Focus 12: a source build, whatever its version string, never
// downloads, and neither do keyed or official builds.
func TestAutoUpgradeNeverDownloadsForASourceBuild(t *testing.T) {
	vcs := []debug.BuildSetting{{Key: "vcs", Value: "git"}}
	cases := map[string]struct {
		info              *debug.BuildInfo
		official, withKey bool
	}{
		"(devel)":               {&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, false, false},
		"checkout at a tag":     {&debug.BuildInfo{Main: debug.Module{Version: "v2.0.0"}, Settings: vcs}, false, false},
		"go install @main":      {&debug.BuildInfo{Main: debug.Module{Version: "v2.0.1-0.20261001120000-4e51705aabbc"}}, false, false},
		"make build with a key": {&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs}, false, true},
		"official release":      {&debug.BuildInfo{Main: debug.Module{Version: "v2.0.0"}, Settings: vcs}, true, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			r.hook.kind, r.hook.tag = selfupdate.Classify(c.info, true, c.official, c.withKey)
			r.hook.beforeRun([]string{"celeste", "chat"})
			<-r.hook.background()
			if n := r.server.Requests(); n != 0 {
				t.Fatalf("%d requests from a %s build", n, kindName(r.hook.kind))
			}
			if len(*r.execs) != 0 || r.exeBytes(t) != "go install build" || r.stderr.Len() != 0 {
				t.Fatalf("execs %d, stderr %q", len(*r.execs), r.stderr.String())
			}
		})
	}
}

// Review Focus 10, ruling 28: stdio protocols and the quick commands
// don't upgrade before running.
func TestAutoUpgradeSkipsServeAndACP(t *testing.T) {
	for _, argv := range [][]string{
		{"celeste", "serve"},
		{"celeste", "-config", "work", "serve"},
		{"celeste", "acp"},
		{"celeste", "help"},
		{"celeste", "--version"},
		{"celeste", "version"},
		{"celeste", "update", "--check"},
		{"celeste", "persona", "verify"}, // a diagnostic reports on this binary, not its upgrade
		{"celeste", "-mode", "classic"},  // a flag error: run() reports it, no upgrade first
	} {
		r := newRig(t, nil)
		r.hook.beforeRun(argv)
		if n := r.server.Requests(); n != 0 || len(*r.execs) != 0 {
			t.Errorf("%v: %d requests, %d re-execs", argv, n, len(*r.execs))
		}
	}
}

func TestAutoUpgradeOptOut(t *testing.T) {
	r := newRig(t, nil)
	r.env["CELESTE_NO_AUTO_UPGRADE"] = "1"
	r.hook.beforeRun([]string{"celeste", "chat"})
	if r.server.Requests() != 0 || len(*r.execs) != 0 {
		t.Fatal("CELESTE_NO_AUTO_UPGRADE=1 still upgraded")
	}
	r.env["CELESTE_NO_AUTO_UPGRADE"] = "0"
	r.hook.beforeRun([]string{"celeste", "chat"})
	if len(*r.execs) != 1 {
		t.Fatal("CELESTE_NO_AUTO_UPGRADE=0 did not upgrade")
	}
}

// Review Focus 9 and ruling 27: a failure replaces nothing, warns once,
// runs on, and waits an hour before trying that tag again.
func TestAutoUpgradeFailureWarnsOnceAndThrottles(t *testing.T) {
	r := newRig(t, func(key *selfupdatetest.Key) map[string][]byte {
		f := selfupdatetest.Build(t, key, upTag, officialBin)
		f["checksums.txt.asc"] = selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0).Sign(t, f["checksums.txt"], time.Time{})
		return f
	})
	r.hook.beforeRun([]string{"celeste", "chat"})
	if r.exeBytes(t) != "go install build" || len(*r.execs) != 0 {
		t.Fatal("a failed upgrade replaced or re-executed")
	}
	if n := strings.Count(r.stderr.String(), "couldn't install the official v2.0.0 build"); n != 1 {
		t.Fatalf("%d warnings: %q", n, r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "tries again in an hour") {
		t.Fatalf("the warning misstates the throttle: %q", r.stderr.String())
	}
	before, stderr := r.server.Requests(), r.stderr.Len()
	r.hook.beforeRun([]string{"celeste", "chat"})
	if r.server.Requests() != before || r.stderr.Len() != stderr {
		t.Fatal("retried within the hour")
	}
	later := time.Now().Add(61 * time.Minute)
	r.hook.throttle.Now = func() time.Time { return later }
	r.hook.beforeRun([]string{"celeste", "chat"})
	if r.server.Requests() == before {
		t.Fatal("did not retry after an hour")
	}
}

// Ruling 26: the re-executed process never tries again.
func TestAutoUpgradeLoopGuard(t *testing.T) {
	r := newRig(t, nil)
	r.env[selfupdatedEnv] = upTag
	r.hook.beforeRun([]string{"celeste", "chat"})
	if r.server.Requests() != 0 || len(*r.execs) != 0 {
		t.Fatal("upgraded again after a re-exec")
	}
	if !strings.Contains(r.stderr.String(), "not trying again") {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
	if r.env[selfupdatedEnv] != upTag {
		t.Fatal("a module build dropped the loop guard")
	}
}

// The official build a re-exec started drops the loop guard, so a go
// install build that one of its child processes starts can still upgrade.
func TestOfficialBuildDropsTheLoopGuard(t *testing.T) {
	r := newRig(t, nil)
	r.hook.kind = selfupdate.Official
	r.env[selfupdatedEnv] = upTag
	r.hook.beforeRun([]string{"celeste", "chat"})
	if _, ok := r.env[selfupdatedEnv]; ok {
		t.Fatalf("%s is still set for child processes", selfupdatedEnv)
	}
	if r.server.Requests() != 0 || len(*r.execs) != 0 || r.stderr.Len() != 0 {
		t.Fatalf("an official build upgraded or warned: %q", r.stderr.String())
	}
}

// Review Focus 10, ruling 28: serve's upgrade installs for the next launch,
// never re-execs, and writes nothing to stdout.
func TestServeUpgradeWritesNothingToStdout(t *testing.T) {
	r := newRig(t, nil)
	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = pw
	<-r.hook.background()
	os.Stdout = stdout
	pw.Close()
	got, _ := io.ReadAll(pr)
	if len(got) != 0 {
		t.Fatalf("stdout = %q", got)
	}
	if len(*r.execs) != 0 {
		t.Fatal("serve re-executed")
	}
	if r.exeBytes(t) != string(officialBin) {
		t.Fatal("serve did not install for the next launch")
	}
	if !strings.Contains(logs.String(), "[update] installed the official v2.0.0 build; the next launch runs it") {
		t.Fatalf("log = %q", logs.String())
	}
}

func TestCommandWord(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"serve":                       "serve",
		"-config work serve --sse":    "serve",
		"-max-tool-iterations 9 chat": "chat",
		"hello there":                 "hello",
	}
	for in, want := range cases {
		got, ok := commandWord(strings.Fields(in))
		if !ok || got != want {
			t.Errorf("commandWord(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := commandWord([]string{"-mode", "classic"}); ok {
		t.Error("a flag error is ok")
	}
	if configName != "" || maxToolIterationsOverride != 0 {
		t.Error("commandWord left global flags set")
	}
}
