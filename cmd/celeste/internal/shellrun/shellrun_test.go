package shellrun

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunPassesStdinAndCapsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	res := Run(context.Background(), Options{Dir: t.TempDir(), Command: "cat", Stdin: []byte(`{"a":1}`), Timeout: 5 * time.Second})
	if res.Output != `{"a":1}` || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	res = Run(context.Background(), Options{Dir: t.TempDir(), Command: "head -c 200000 /dev/zero | tr '\\0' x", Timeout: 5 * time.Second})
	if !res.Truncated || len(res.Output) != DefaultMaxOutput {
		t.Fatalf("default cap: truncated=%v len=%d", res.Truncated, len(res.Output))
	}
	res = Run(context.Background(), Options{Dir: t.TempDir(), Command: "head -c 5000 /dev/zero | tr '\\0' x", Timeout: 5 * time.Second, MaxOutput: 100})
	if !res.Truncated || len(res.Output) != 100 {
		t.Fatalf("MaxOutput: truncated=%v len=%d", res.Truncated, len(res.Output))
	}
}

func TestRunReportsAnExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	res := Run(context.Background(), Options{Dir: t.TempDir(), Command: "echo out; echo err >&2; exit 3", Timeout: 5 * time.Second})
	if res.ExitCode != 3 || res.Err != nil || res.TimedOut || res.Output != "out\nerr\n" {
		t.Fatalf("result = %+v", res)
	}
}

// Args mode runs on every platform, Windows included, where the held-pipe
// path relies on closing the read end: a direct program run returns its
// output and exit code (review of cleanup-5c).
func TestRunArgsRunsAProgramOnEveryPlatform(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	res := Run(context.Background(), Options{Dir: t.TempDir(), Args: []string{exe, "-test.run=^$"}, Timeout: 30 * time.Second})
	if res.ExitCode != 0 || res.Err != nil || res.TimedOut || !strings.Contains(res.Output, "PASS") {
		t.Fatalf("result = %+v", res)
	}
}
