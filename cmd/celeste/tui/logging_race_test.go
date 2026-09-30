package tui

import (
	"sync"
	"testing"
)

// Runs and the /agent and /orch goroutines log while the chat starts and
// stops logging; the logger must be safe for that (go test -race, F2d).
func TestLoggingIsSafeWhileItStartsAndStops(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				LogInfo("from a run")
			}
		}
	}()
	for i := 0; i < 20; i++ {
		if err := InitLogging(); err != nil {
			t.Fatal(err)
		}
		_ = GetLogPath()
		CloseLogging()
	}
	close(stop)
	wg.Wait()
}
