package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// IsGlobalConfig reports whether path is one of home's GlobalConfigPaths:
// the user's own config, whose servers start without approval. With no
// home, nothing is.
func IsGlobalConfig(home, path string) bool {
	return home != "" && isGlobalConfig(home, path)
}

// TrustHash is what a workspace server's approval is pinned to: its
// transport, command, args, env (names and values as written, before
// ${VAR} expansion) and url. Any edit to them asks again; "enabled",
// "trusted" and the file it sits in do not count.
func (c ServerConfig) TrustHash() string {
	data, _ := json.Marshal(struct {
		Transport string            `json:"transport"`
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		Env       map[string]string `json:"env"` // marshalled with sorted keys
		URL       string            `json:"url"`
	}{c.Transport, c.Command, c.Args, c.Env, c.URL})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TrustSummary describes the server for an approval prompt: what it runs
// or connects to. Strings from the file are Go-quoted so they cannot act
// on the terminal; env values, a URL's password and its query are left
// out, since they often hold secrets.
func (c ServerConfig) TrustSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "transport: %s\n", strconv.Quote(c.Transport))
	if c.Command != "" {
		fmt.Fprintf(&b, "command: %s\n", strconv.Quote(c.Command))
	}
	if len(c.Args) > 0 {
		quoted := make([]string, len(c.Args))
		for i, a := range c.Args {
			quoted[i] = strconv.Quote(a)
		}
		fmt.Fprintf(&b, "args: %s\n", strings.Join(quoted, " "))
	}
	if len(c.Env) > 0 {
		var names []string
		for k := range c.Env {
			names = append(names, strconv.Quote(k))
		}
		slices.Sort(names)
		fmt.Fprintf(&b, "env (values not shown): %s\n", strings.Join(names, " "))
	}
	if c.URL != "" {
		fmt.Fprintf(&b, "url: %s\n", strconv.Quote(summaryURL(c.URL)))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// summaryURL is raw without its userinfo, query and fragment.
func summaryURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(not a valid URL)"
	}
	s := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		s += "?(query not shown)"
	}
	return s
}
