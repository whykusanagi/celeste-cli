package rules

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
)

// Trusted returns the grimoire sections whose stream rules may load (2.0
// W3, ruling 18). A repo grimoire's "## Stream Rules" can stop replies,
// force paid re-runs and inject reminders, so it is trusted like a repo
// hook: by content hash in the hooks trust store (~/.celeste/trusted.json,
// `celeste hooks trust`). The global grimoire (~/.celeste/grimoire.md) is
// the user's own file and needs no trust; neither do the built-ins and
// ~/.celeste/rules, which never pass through here.
//
// approve is the interactive chat's prompt (the same one repo hooks use);
// a yes is recorded in the store. Non-interactive runs pass nil: only
// sections already trusted load, and the rest are skipped with one warning.
func Trusted(home string, secs []Section, approve hooks.ApproveFunc, warn func(string)) []Section {
	if len(secs) == 0 {
		return nil
	}
	if warn == nil {
		warn = func(string) {}
	}
	// One file's sections are trusted together, in order.
	var order []string
	bodies := map[string][]string{}
	for _, s := range secs {
		if _, ok := bodies[s.Source]; !ok {
			order = append(order, s.Source)
		}
		bodies[s.Source] = append(bodies[s.Source], strings.TrimSpace(s.Body))
	}
	var global string
	if home != "" {
		global = filepath.Join(home, ".celeste", "grimoire.md")
	}
	var store *hooks.TrustStore
	allowed := map[string]bool{}
	var skipped []string
	for _, path := range order {
		if global != "" && samePath(path, global) {
			allowed[path] = true
			continue
		}
		if home == "" {
			skipped = append(skipped, strconv.Quote(path)+" (no home directory for the trust store)")
			continue
		}
		if store == nil {
			store = hooks.LoadTrust(home)
			if err := store.Err(); err != nil {
				warn(fmt.Sprintf("stream rules: %v; repo stream rules stay untrusted until it is fixed or removed", err))
			}
		}
		if err := hooks.CheckRepoGrimoire(path); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", strconv.Quote(path), err))
			continue
		}
		src := hooks.StreamRulesSource(path, strings.Join(bodies[path], "\n"))
		status := store.Status(src)
		switch {
		case status == hooks.Trusted:
			allowed[path] = true
		case approve != nil && store.Err() == nil && approve(src, status):
			allowed[path] = true
			if err := store.Approve(src); err != nil {
				warn(fmt.Sprintf("stream rules: %s approved for this session only: %v", strconv.Quote(path), err))
			}
		case status == hooks.Changed:
			skipped = append(skipped, strconv.Quote(path)+" (changed since you approved it)")
		default:
			skipped = append(skipped, strconv.Quote(path)+" (not trusted)")
		}
	}
	if len(skipped) > 0 {
		warn(fmt.Sprintf("stream rules: skipping the \"## Stream Rules\" section in %s; run `celeste hooks trust` to approve them", strings.Join(skipped, ", ")))
	}
	var out []Section
	for _, s := range secs {
		if allowed[s.Source] {
			out = append(out, s)
		}
	}
	return out
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
