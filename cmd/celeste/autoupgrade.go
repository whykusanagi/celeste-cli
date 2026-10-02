package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate"
)

// selfupdatedEnv marks a process started by a self-upgrade, so it never
// tries again (W5 ruling 26).
const selfupdatedEnv = "CELESTE_SELFUPDATED"

// hasPersonaKey reports whether this binary carries the persona key, which
// makes a local build Keyed (W5 ruling 25). It is a seam until W5-A's
// prompts.HasPersonaKey lands: at that merge, replace this with
// prompts.HasPersonaKey. Until then no build injects a key, so false is exact.
var hasPersonaKey = func() bool { return false }

// noUpgradeCommands print and exit, or (update) upgrade on their own.
var noUpgradeCommands = map[string]bool{
	"help": true, "-h": true, "--help": true,
	"version": true, "-v": true, "--version": true,
	"update": true,
}

// stdioCommands own stdout for a protocol, so they upgrade in the
// background for the next launch (ruling 28). acp is W4's.
var stdioCommands = map[string]bool{"serve": true, "acp": true}

// upgradeHook is W5 rulings 25–28 at startup: a `go install` build replaces
// itself with the official release binary of the same tag and re-executes,
// so the same invocation runs the full persona.
type upgradeHook struct {
	kind     selfupdate.Kind
	tag      string
	getenv   func(string) string
	updater  *selfupdate.Updater
	throttle *selfupdate.Throttle // nil: no home directory; never throttled
	reexec   func(exe string, argv, env []string) error
	environ  func() []string
	stderr   io.Writer
	timeout  time.Duration
}

func newUpgradeHook() *upgradeHook {
	info, ok := debug.ReadBuildInfo()
	kind, tag := selfupdate.Classify(info, ok, Channel == "release", hasPersonaKey())
	th, _ := selfupdate.DefaultThrottle()
	return &upgradeHook{
		kind: kind, tag: tag, getenv: os.Getenv, updater: selfupdate.New(), throttle: th,
		reexec: selfupdate.Reexec, environ: os.Environ, stderr: os.Stderr, timeout: 2 * time.Minute,
	}
}

// commandWord is the command run() will dispatch, after the global flags;
// false when the flags don't parse (run reports that, with no upgrade).
func commandWord(args []string) (string, bool) {
	defer resetGlobalFlags()
	rest, err := extractGlobalFlags(args, io.Discard)
	if err != nil {
		return "", false
	}
	if len(rest) == 0 {
		return "", true // the chat
	}
	return rest[0], true
}

// enabled reports whether this process may upgrade now (rulings 25–27).
func (h *upgradeHook) enabled() bool {
	if h.kind != selfupdate.Module {
		return false
	}
	if v := h.getenv("CELESTE_NO_AUTO_UPGRADE"); v != "" && v != "0" {
		return false
	}
	if prev := h.getenv(selfupdatedEnv); prev != "" {
		fmt.Fprintf(h.stderr, "celeste: still a go install build after upgrading to %s; not trying again in this process\n", prev)
		return false
	}
	return h.throttle == nil || h.throttle.Due(h.tag)
}

// beforeRun runs first in main. It returns when this process should go on
// as it is: not a module build, a command that doesn't upgrade first, or a
// failed upgrade (one warning; the public persona runs). After a successful
// install it re-executes argv with the official binary and, on unix, does
// not return.
func (h *upgradeHook) beforeRun(argv []string) {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			selfupdate.CleanupOld(exe)
		}
	}
	cmd, ok := commandWord(argv[1:])
	if !ok || noUpgradeCommands[cmd] || stdioCommands[cmd] || !h.enabled() {
		return
	}
	fmt.Fprintf(h.stderr, "celeste: installing the official %s build for %s/%s (one time; CELESTE_NO_AUTO_UPGRADE=1 skips this)\n", h.tag, h.updater.GOOS, h.updater.GOARCH)
	exe, err := h.upgrade()
	if err != nil {
		fmt.Fprintf(h.stderr, "celeste: couldn't install the official %s build: %v. Running the public persona; celeste tries again within the hour.\n", h.tag, err)
		return
	}
	fmt.Fprintf(h.stderr, "celeste: installed the official %s build; starting it\n", h.tag)
	env := append(h.environ(), selfupdatedEnv+"="+h.tag)
	if err := h.reexec(exe, argv, env); err != nil {
		fmt.Fprintf(h.stderr, "celeste: couldn't start the official build (%v); it runs from the next launch\n", err)
	}
}

// background is serve's and acp's upgrade (ruling 28): it installs the
// official build for the next launch, never re-executes, and writes only
// through log (stderr); stdout belongs to the protocol. The channel closes
// when it is done.
func (h *upgradeHook) background() <-chan struct{} {
	done := make(chan struct{})
	if !h.enabled() {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if _, err := h.upgrade(); err != nil {
			log.Printf("[update] installing the official %s build failed: %v; this session runs the public persona", h.tag, err)
			return
		}
		log.Printf("[update] installed the official %s build; the next launch runs it", h.tag)
	}()
	return done
}

// upgrade installs the official build of h.tag and keeps the throttle.
func (h *upgradeHook) upgrade() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	exe, err := h.updater.Upgrade(ctx, h.tag)
	if h.throttle != nil {
		if err != nil {
			_ = h.throttle.Record(h.tag)
		} else {
			h.throttle.Clear()
		}
	}
	return exe, err
}
