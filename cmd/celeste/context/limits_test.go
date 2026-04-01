package ctxmgr

import (
	"os"
	"path/filepath"
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
