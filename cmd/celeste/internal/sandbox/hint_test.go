package sandbox

import (
	"strings"
	"testing"
)

func TestHintNamesTheSandboxAndTheKey(t *testing.T) {
	p := Policy{Enabled: true, Network: true}
	for _, out := range []string{
		"sh: /etc/x: Operation not permitted",
		"touch: cannot touch '/usr/x': Read-only file system",
		"Sandbox: sh(123) deny(1) file-write-create /x",
		// Go tools print errno text in lower case.
		"verifying module: open /home/u/go/pkg/sumdb/sum.golang.org/latest: operation not permitted",
		"open /usr/x: read-only file system",
	} {
		h := Hint(KindSeatbelt, p, out)
		if !strings.Contains(h, "seatbelt") || !strings.Contains(h, `"sandbox.writable"`) || !strings.Contains(h, `"sandbox.enabled": false`) || !strings.Contains(h, "celeste hooks trust") {
			t.Errorf("Hint(%q) = %q", out, h)
		}
	}
	if h := Hint(KindBubblewrap, p, "Could not resolve host: example.com"); h != "" {
		t.Errorf("network on: no network hint, got %q", h)
	}
	if h := Hint(KindBubblewrap, p, "error: tests failed"); h != "" {
		t.Errorf("an unrelated failure gets no hint, got %q", h)
	}
	p.Network = false
	for _, out := range []string{
		"curl: (6) Could not resolve host: example.com",
		"Temporary failure in name resolution",
		"connect: Network is unreachable",
		"getaddrinfo: nodename nor servname provided, or not known",
		"dial tcp: lookup proxy.golang.org: no such host",
		"dial tcp 1.2.3.4:443: connect: network is unreachable",
	} {
		h := Hint(KindBubblewrap, p, out)
		if !strings.Contains(h, "bubblewrap") || !strings.Contains(h, `"sandbox.network": false`) {
			t.Errorf("Hint(%q) = %q", out, h)
		}
	}
}
