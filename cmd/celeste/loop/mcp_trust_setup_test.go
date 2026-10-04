package loop

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
)

// chatSetup runs a chat Setup in ws with approve as its approver.
func chatSetup(t *testing.T, ws string, approve hooks.ApproveFunc) *warnings {
	t.Helper()
	w := &warnings{}
	env, err := Setup(ModeChat, testCfg(), ws, SetupOptions{Warn: w.add, Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return w
}

func skipWithoutSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
}

// A cloned repo's enabled MCP server does not start in the chat until the
// person approves it: with nobody to ask it stays pending, and the warning
// says how to approve it.
func TestChatPendingWorkspaceMCPDoesNotStart(t *testing.T) {
	skipWithoutSh(t)
	setupHome(t)
	ws := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	cfgPath := filepath.Join(ws, ".mcp.json")
	write(t, cfgPath, markerMCPConfig(marker))

	w := chatSetup(t, ws, nil)
	if waitForFile(marker, 300*time.Millisecond) {
		t.Fatal("the chat started a workspace MCP server nobody approved")
	}
	got := w.all()
	for _, want := range []string{strconv.Quote("probe"), strconv.Quote(cfgPath), "celeste hooks trust", "/mcp"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning lacks %q:\n%s", want, got)
		}
	}
}

// Declining keeps it from starting, and nothing is recorded.
func TestChatDeclinedWorkspaceMCPDoesNotStart(t *testing.T) {
	skipWithoutSh(t)
	home := setupHome(t)
	ws := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	write(t, filepath.Join(ws, ".celeste", "mcp.json"), markerMCPConfig(marker))

	var asked []hooks.Source
	chatSetup(t, ws, func(src hooks.Source, _ hooks.TrustStatus) bool {
		asked = append(asked, src)
		return false
	})
	if waitForFile(marker, 300*time.Millisecond) {
		t.Fatal("a declined workspace MCP server started")
	}
	if len(asked) != 1 || asked[0].Kind != hooks.KindRepoMCP {
		t.Fatalf("asked = %+v, want one %s question", asked, hooks.KindRepoMCP)
	}
	if st := hooks.LoadTrust(home).Status(asked[0]); st != hooks.Untrusted {
		t.Fatalf("status after declining = %s, want untrusted", st)
	}
}

// An approved server starts, its approval is stored, and the next launch
// starts it without asking.
func TestChatApprovedWorkspaceMCPStarts(t *testing.T) {
	skipWithoutSh(t)
	home := setupHome(t)
	ws := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	write(t, filepath.Join(ws, ".mcp.json"), markerMCPConfig(marker))

	var asked []hooks.Source
	chatSetup(t, ws, func(src hooks.Source, _ hooks.TrustStatus) bool {
		asked = append(asked, src)
		return true
	})
	if !waitForFile(marker, 5*time.Second) {
		t.Fatal("an approved workspace MCP server did not start")
	}
	if len(asked) != 1 || hooks.LoadTrust(home).Status(asked[0]) != hooks.Trusted {
		t.Fatalf("approval not stored: asked %+v", asked)
	}
	// The description shown for approval names the command.
	if !strings.Contains(asked[0].Rules, `"sh"`) {
		t.Errorf("approval text lacks the command:\n%s", asked[0].Rules)
	}

	chatSetup(t, ws, func(src hooks.Source, _ hooks.TrustStatus) bool {
		t.Errorf("asked again about an approved server: %s", src.Path)
		return false
	})
}

// Editing an approved server's command or args puts it back to pending.
func TestChatEditedWorkspaceMCPResetsToPending(t *testing.T) {
	skipWithoutSh(t)
	setupHome(t)
	ws := t.TempDir()
	cfgPath := filepath.Join(ws, ".mcp.json")
	first := filepath.Join(t.TempDir(), "first")
	write(t, cfgPath, markerMCPConfig(first))
	chatSetup(t, ws, func(hooks.Source, hooks.TrustStatus) bool { return true })
	if !waitForFile(first, 5*time.Second) {
		t.Fatal("the approved server did not start")
	}

	edited := filepath.Join(t.TempDir(), "edited")
	write(t, cfgPath, markerMCPConfig(edited))
	var status []hooks.TrustStatus
	chatSetup(t, ws, func(src hooks.Source, st hooks.TrustStatus) bool {
		status = append(status, st)
		return false
	})
	if waitForFile(edited, 300*time.Millisecond) {
		t.Fatal("an edited server started on its old approval")
	}
	if len(status) != 1 || status[0] != hooks.Changed {
		t.Fatalf("asked with %v, want [changed]", status)
	}
	w := chatSetup(t, ws, nil)
	if !strings.Contains(w.all(), "changed") {
		t.Errorf("warning does not say the server changed:\n%s", w.all())
	}
}

// A home-level config's server is the user's own: never asked about.
func TestChatHomeMCPNeedsNoApproval(t *testing.T) {
	skipWithoutSh(t)
	home := setupHome(t)
	marker := filepath.Join(t.TempDir(), "started")
	write(t, filepath.Join(home, ".celeste", "mcp.json"), markerMCPConfig(marker))
	chatSetup(t, t.TempDir(), func(src hooks.Source, _ hooks.TrustStatus) bool {
		if src.Kind == hooks.KindRepoMCP {
			t.Errorf("asked about a home-level server: %s", src.Path)
		}
		return false
	})
	if !waitForFile(marker, 5*time.Second) {
		t.Fatal("the home-level server did not start")
	}
}

// A disabled workspace server is not asked about: it never starts on its own.
func TestChatDisabledWorkspaceMCPIsNotAsked(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".mcp.json"), `{"mcpServers":{"off":{"command":"sh"}}}`)
	chatSetup(t, ws, func(src hooks.Source, _ hooks.TrustStatus) bool {
		t.Errorf("asked about a disabled server: %s", src.Path)
		return false
	})
}
