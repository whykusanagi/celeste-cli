package ctxmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapToolResult_UnderLimit(t *testing.T) {
	result := "short result"
	capped, wasCapped, err := CapToolResult(result, 1024, "sess1", "tc1", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasCapped {
		t.Error("wasCapped should be false for short result")
	}
	if capped != result {
		t.Errorf("capped = %q, want %q", capped, result)
	}
}

func TestCapToolResult_OverLimit(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a 64KB result
	result := strings.Repeat("x", 64*1024)

	capped, wasCapped, err := CapToolResult(result, 32*1024, "sess1", "tc42", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasCapped {
		t.Error("wasCapped should be true for oversized result")
	}

	// Verify capped result contains truncation notice
	if !strings.Contains(capped, "TRUNCATED") {
		t.Error("capped result should contain TRUNCATED notice")
	}
	if !strings.Contains(capped, "tc42.txt") {
		t.Error("capped result should contain spill file path")
	}
	if !strings.Contains(capped, "65536 bytes total") {
		t.Error("capped result should contain total byte count")
	}

	// Verify the spill file was written with full content
	spillPath := filepath.Join(tmpDir, "sess1", "tc42.txt")
	data, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("failed to read spill file: %v", err)
	}
	if len(data) != 64*1024 {
		t.Errorf("spill file size = %d, want %d", len(data), 64*1024)
	}
}

func TestCapToolResult_ExactlyAtLimit(t *testing.T) {
	result := strings.Repeat("a", 1024)
	capped, wasCapped, err := CapToolResult(result, 1024, "s", "t", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasCapped {
		t.Error("should not cap result exactly at limit")
	}
	if capped != result {
		t.Error("result should be unchanged when exactly at limit")
	}
}

func TestCapToolResult_CreatesSessionDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := strings.Repeat("z", 2048)

	_, _, err := CapToolResult(result, 512, "new-session", "tc1", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := filepath.Join(tmpDir, "new-session")
	info, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("session dir should be a directory")
	}
}

// A spilled tool result may hold secrets (env dumps, API responses, file
// contents), so the session dir and the spill file must be private to the
// owner, not world/group readable.
func TestCapToolResult_SpillIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	tmpDir := t.TempDir()
	result := strings.Repeat("x", 64*1024)

	if _, _, err := CapToolResult(result, 32*1024, "sess-priv", "tc-priv", tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Join(tmpDir, "sess-priv"))
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("session dir mode = %o, want 0700", perm)
	}

	fileInfo, err := os.Stat(filepath.Join(tmpDir, "sess-priv", "tc-priv.txt"))
	if err != nil {
		t.Fatalf("stat spill file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0600 {
		t.Errorf("spill file mode = %o, want 0600", perm)
	}
}

// MkdirAll/WriteFile only apply their mode to a path they create, so a
// session dir or spill file left over from an old binary (or a reused
// sessionID/toolCallID) must still end up private, not just newly-created
// ones.
func TestCapToolResult_TightensExistingLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sess-old")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	spillPath := filepath.Join(sessionDir, "tc-old.txt")
	if err := os.WriteFile(spillPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result := strings.Repeat("x", 64*1024)
	if _, _, err := CapToolResult(result, 32*1024, "sess-old", "tc-old", tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dirInfo, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("pre-existing session dir mode = %o, want 0700 after a spill", perm)
	}
	fileInfo, err := os.Stat(spillPath)
	if err != nil {
		t.Fatalf("stat spill file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0600 {
		t.Errorf("pre-existing spill file mode = %o, want 0600 after being overwritten", perm)
	}
}

func TestCapToolResult_DefaultMaxBytes(t *testing.T) {
	// Pass 0 for maxBytes -- should use DefaultMaxToolResultBytes
	result := strings.Repeat("y", DefaultMaxToolResultBytes+100)
	_, wasCapped, err := CapToolResult(result, 0, "s", "t", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasCapped {
		t.Error("should cap when using default and result exceeds it")
	}
}

// SnipToolResult is CapToolResult without the spill file (2.0 F3): the loop's
// fallback when the spill fails, and the cap on histories loaded from disk.
// It never returns more than maxBytes and marks the cut.
func TestSnipToolResult(t *testing.T) {
	if got := SnipToolResult("short", 1024); got != "short" {
		t.Fatalf("under the cap the text must come back unchanged, got %q", got)
	}
	exact := strings.Repeat("e", 4096)
	if got := SnipToolResult(exact, 4096); got != exact {
		t.Fatal("a text exactly at the cap must come back unchanged")
	}
	for _, max := range []int{1024, 4096, DefaultMaxToolResultBytes} {
		text := "HEAD" + strings.Repeat("x", 3*max) + "TAIL"
		got := SnipToolResult(text, max)
		if len(got) > max {
			t.Fatalf("max %d: got %d bytes", max, len(got))
		}
		if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") {
			t.Fatalf("max %d: head and tail must both be kept", max)
		}
		if !strings.Contains(got, "snipped") {
			t.Fatalf("max %d: the cut carries no marker", max)
		}
	}
	// A cut never splits a UTF-8 sequence.
	got := SnipToolResult(strings.Repeat("é", 4096), 1024)
	if !utf8.ValidString(got) {
		t.Fatal("the cut split a multi-byte character")
	}
}

// #211: the spill notice names the id recall_tool_result takes, and that id
// finds the spilled file again; ids that are not <session>/<name> of safe
// names are refused.
func TestSpillNoticeNamesRecallID(t *testing.T) {
	base := t.TempDir()
	full := strings.Repeat("r", 4096)
	capped, wasCapped, err := CapToolResult(full, 1024, "sess-1", "call_2-1", base)
	if err != nil || !wasCapped {
		t.Fatalf("cap: %v %v", wasCapped, err)
	}
	if !strings.Contains(capped, `recall_tool_result with id "sess-1/call_2-1"`) {
		t.Fatalf("the notice does not name the recall id: %q", capped)
	}
	if !strings.Contains(capped, "full output saved to: "+filepath.Join(base, "sess-1", "call_2-1.txt")) {
		t.Fatal("the notice lost its spill path")
	}
	got, err := LoadSpilled(base, "sess-1/call_2-1")
	if err != nil || got != full {
		t.Fatalf("LoadSpilled = %d bytes, %v; want the full %d", len(got), err, len(full))
	}
	for _, bad := range []string{"", "sess-1", "../x", "a/../b", "a/b/c", "a/b.txt", "./b", `a\b`} {
		if _, err := LoadSpilled(base, bad); err == nil {
			t.Errorf("LoadSpilled(%q) succeeded", bad)
		}
	}
	if _, err := LoadSpilled(base, "sess-1/missing"); err == nil {
		t.Error("a missing spill file must be an error")
	}
}
