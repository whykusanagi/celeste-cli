package ctxmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
