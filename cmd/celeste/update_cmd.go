package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/selfupdate"
)

// newUpdater and updateKind are variables so tests point `celeste update`
// at a fake release.
var (
	newUpdater = selfupdate.New
	updateKind = func() (selfupdate.Kind, string) {
		info, ok := debug.ReadBuildInfo()
		kind, tag := selfupdate.Classify(info, ok, Channel == "release", prompts.HasPersonaKey())
		if kind == selfupdate.Official {
			tag = "v" + Version
		}
		return kind, tag
	}
)

// runUpdateCommand is `celeste update [--check]` (W5 ruling 29).
func runUpdateCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "")
	// Maintainer-only (release.yml, W5 ruling 30): verify a release
	// directory with this binary's embedded key, the updater's own checks.
	verifyDist := fs.String("verify-dist", "", "")
	distTag := fs.String("tag", "", "")
	usage := func() int {
		fmt.Fprintln(stderr, "Usage: celeste update [--check]")
		return 2
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usage()
	}
	if *verifyDist != "" {
		if *distTag == "" {
			fmt.Fprintln(stderr, "Usage: celeste update --verify-dist <dir> --tag <tag>")
			return 2
		}
		if err := selfupdate.VerifyDist(*verifyDist, *distTag, selfupdate.ReleaseKey); err != nil {
			fmt.Fprintf(stderr, "celeste update --verify-dist: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "release %s verifies with the embedded release key\n", *distTag)
		return 0
	}
	kind, current := updateKind()
	if kind != selfupdate.Official && kind != selfupdate.Module {
		fmt.Fprintln(stderr, "celeste update: this binary was built from source; update your checkout (git pull) and rebuild (make install)")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := newUpdater()
	latest, err := u.LatestTag(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "celeste update: %v\n", err)
		return 1
	}
	newer := selfupdate.Newer(latest, current)
	if *check || (kind == selfupdate.Official && !newer) {
		if newer {
			fmt.Fprintf(stdout, "celeste %s is available (this is %s); run celeste update\n", latest, current)
		} else {
			fmt.Fprintf(stdout, "celeste %s is the latest release\n", current)
		}
		return 0
	}
	target := current
	if newer {
		target = latest
	}
	exe, err := u.Upgrade(ctx, target)
	if err != nil {
		fmt.Fprintf(stderr, "celeste update: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "installed the official celeste %s at %s\n", target, exe)
	return 0
}
