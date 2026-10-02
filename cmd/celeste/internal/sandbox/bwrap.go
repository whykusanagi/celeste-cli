package sandbox

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// BwrapArgs is the bubblewrap argument list (everything after "bwrap")
// that runs command under p: the whole filesystem read-only, a fresh /dev
// and /proc, /run hidden behind a tmpfs (its sockets, the docker and
// session buses among them, reach outside the sandbox) except the systemd
// resolver directory /etc/resolv.conf often points into, then each
// writable directory that exists bound read-write. --unshare-pid is what
// lets an unprivileged bwrap mount /proc; --new-session takes the command
// off the terminal. bwrap itself leads the runner's new session and
// process group, so a timeout kills it whole.
func BwrapArgs(p Policy, command string) []string {
	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/run",
		"--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve",
	}
	for _, dir := range p.Writable {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			args = append(args, "--bind", dir, dir)
		}
	}
	if !p.Network {
		args = append(args, "--unshare-net")
	}
	// --new-session: no controlling terminal to type into (TIOCSTI,
	// CVE-2017-5226).
	args = append(args, "--unshare-pid", "--new-session", "--die-with-parent")
	if p.Workspace != "" {
		args = append(args, "--chdir", p.Workspace)
	}
	return append(args, "--", "sh", "-c", command)
}

// probeTimeout bounds one self-test run of a sandbox program.
const probeTimeout = 5 * time.Second

// bwrapProbe reports whether the bwrap at path can build the sandbox
// BwrapArgs asks for. Where unprivileged user namespaces are disabled
// (many containers and CI runners) it cannot, and the sandbox counts as
// missing. A var so tests can make it fail.
var bwrapProbe = func(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, path, BwrapArgs(Policy{Network: false}, "true")...).Run() == nil
}

// seatbeltProbe reports whether sandbox-exec can apply a profile here; it
// cannot inside another seatbelt sandbox.
var seatbeltProbe = func(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, path, "-p", "(version 1)\n(allow default)\n", "/usr/bin/true").Run() == nil
}
