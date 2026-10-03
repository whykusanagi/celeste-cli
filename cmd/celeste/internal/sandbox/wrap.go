package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
)

// The sandbox kinds Available reports, also named in blocked-write hints.
const (
	KindSeatbelt   = "seatbelt"
	KindBubblewrap = "bubblewrap"
)

var avail struct {
	mu      sync.Mutex
	checked bool
	kind    string
	path    string // the sandbox program
}

// Available reports which OS sandbox this process can use: "seatbelt" on
// macOS when sandbox-exec works, "bubblewrap" on Linux when bwrap is
// installed and its self-test passes; false elsewhere (Windows has none).
// Checked once per process.
func Available() (kind string, ok bool) {
	avail.mu.Lock()
	defer avail.mu.Unlock()
	if !avail.checked {
		avail.kind, avail.path = detect()
		avail.checked = true
	}
	return avail.kind, avail.kind != ""
}

func detect() (kind, path string) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat(sandboxExec); err == nil && seatbeltProbe(sandboxExec) {
			return KindSeatbelt, sandboxExec
		}
	case "linux":
		if p, err := exec.LookPath("bwrap"); err == nil && bwrapProbe(p) {
			return KindBubblewrap, p
		}
	}
	return "", ""
}

// resetAvailable forgets the cached check (tests).
func resetAvailable() {
	avail.mu.Lock()
	avail.checked, avail.kind, avail.path = false, "", ""
	avail.mu.Unlock()
}

// SetAvailableForTest makes Available report kind and ok (bubblewrap runs
// as "bwrap" from PATH) until the returned function restores the real
// check. For tests in other packages.
func SetAvailableForTest(kind string, ok bool) func() {
	avail.mu.Lock()
	defer avail.mu.Unlock()
	avail.checked, avail.kind, avail.path = true, "", ""
	if ok {
		avail.kind = kind
		avail.path = sandboxExec
		if kind == KindBubblewrap {
			avail.path = "bwrap"
		}
	}
	return resetAvailable
}

// Wrap returns the argv that runs command under p in the available
// sandbox, and that sandbox's kind. It returns nil and "" when p is
// disabled or no sandbox is available: the caller runs sh -c command
// itself.
func Wrap(p Policy, command string) (argv []string, kind string) {
	if !p.Enabled {
		return nil, ""
	}
	kind, ok := Available()
	if !ok {
		return nil, ""
	}
	avail.mu.Lock()
	path := avail.path
	avail.mu.Unlock()
	switch kind {
	case KindSeatbelt:
		return []string{path, "-p", Profile(p), "sh", "-c", command}, kind
	case KindBubblewrap:
		return append([]string{path}, BwrapArgs(p, command)...), kind
	}
	return nil, ""
}
