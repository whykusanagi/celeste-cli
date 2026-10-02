package sandbox

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestBwrapArgs(t *testing.T) {
	orig := resolvConf
	resolvConf = filepath.Join(t.TempDir(), "resolv.conf") // not under /run
	t.Cleanup(func() { resolvConf = orig })
	a, b := t.TempDir(), t.TempDir()
	missing := a + "-missing"
	p := Policy{Enabled: true, Workspace: a, Writable: []string{a, b, missing}, Network: false}
	got := BwrapArgs(p, "echo hi")
	want := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/run",
		"--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve",
		"--bind", a, a,
		"--bind", b, b,
		"--unshare-net",
		"--unshare-pid",
		"--new-session",
		"--die-with-parent",
		"--chdir", a,
		"--", "sh", "-c", "echo hi",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("BwrapArgs =\n%q\nwant\n%q", got, want)
	}
	p.Network = true
	if slices.Contains(BwrapArgs(p, "true"), "--unshare-net") {
		t.Fatal("network on: no --unshare-net")
	}
}

// Review Minor 6: a resolver file under /run other than systemd's
// (NetworkManager, resolvconf) stays visible behind the /run tmpfs.
func TestBwrapKeepsTheResolverDirUnderRun(t *testing.T) {
	for target, want := range map[string]string{
		"/run/NetworkManager/resolv.conf":       "/run/NetworkManager",
		"/run/resolvconf/resolv.conf":           "/run/resolvconf",
		"/run/systemd/resolve/stub-resolv.conf": "",                 // already bound
		"/run/resolv.conf":                      "/run/resolv.conf", // never all of /run
		"/etc/resolv.conf":                      "",
		"/runner/resolv.conf":                   "",
	} {
		if got := runResolverDir(target); got != want {
			t.Errorf("runResolverDir(%s) = %q, want %q", target, got, want)
		}
	}
}

// Review Focus 5.
func TestBwrapUnusableCountsAsMissing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bubblewrap is Linux's")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	orig := bwrapProbe
	bwrapProbe = func(string) bool { return false }
	t.Cleanup(func() { bwrapProbe = orig; resetAvailable() })
	resetAvailable()
	if kind, ok := Available(); ok {
		t.Fatalf("an unusable bwrap counted as available (%s)", kind)
	}
}

func TestAvailableIsNeverTrueOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows")
	}
	if kind, ok := Available(); ok {
		t.Fatalf("Windows has no sandbox, got %s", kind)
	}
}

func TestWrapDisabledIsPlainShell(t *testing.T) {
	t.Cleanup(SetAvailableForTest(KindSeatbelt, true))
	if argv, kind := Wrap(Policy{Enabled: false, Writable: []string{"/x"}}, "echo hi"); argv != nil || kind != "" {
		t.Fatalf("disabled: Wrap = %q, %q", argv, kind)
	}
	t.Cleanup(SetAvailableForTest("", false))
	if argv, kind := Wrap(Policy{Enabled: true, Writable: []string{"/x"}}, "echo hi"); argv != nil || kind != "" {
		t.Fatalf("unavailable: Wrap = %q, %q", argv, kind)
	}
}

func TestWrapUsesTheAvailableSandbox(t *testing.T) {
	p := Policy{Enabled: true, Workspace: "/ws", Writable: []string{"/ws"}, Network: true}

	restore := SetAvailableForTest(KindSeatbelt, true)
	argv, kind := Wrap(p, "echo hi")
	restore()
	want := []string{sandboxExec, "-p", Profile(p), "sh", "-c", "echo hi"}
	if kind != KindSeatbelt || !slices.Equal(argv, want) {
		t.Fatalf("seatbelt: Wrap = %q, %q", argv, kind)
	}

	restore = SetAvailableForTest(KindBubblewrap, true)
	argv, kind = Wrap(p, "echo hi")
	restore()
	if kind != KindBubblewrap || len(argv) == 0 || argv[0] != "bwrap" || !slices.Equal(argv[1:], BwrapArgs(p, "echo hi")) {
		t.Fatalf("bubblewrap: Wrap = %q, %q", argv, kind)
	}
}
