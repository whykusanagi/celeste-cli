package codegraph

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Throwaway timing probe for #385 (never merged).
var probeOn = os.Getenv("CELESTE_CG_PROBE") != ""

var probeMu sync.Mutex
var probeAcc = map[string]time.Duration{}

func probe(name string) func() {
	if !probeOn {
		return func() {}
	}
	start := time.Now()
	return func() {
		fmt.Fprintf(os.Stderr, "cgprobe: %-28s %v\n", name, time.Since(start))
	}
}

// probeAdd accumulates a phase spread across many calls.
func probeAdd(name string, start time.Time) {
	if !probeOn {
		return
	}
	probeMu.Lock()
	probeAcc[name] += time.Since(start)
	probeMu.Unlock()
}

func probeFlush(prefix string) {
	if !probeOn {
		return
	}
	probeMu.Lock()
	for k, v := range probeAcc {
		fmt.Fprintf(os.Stderr, "cgprobe: %s %-24s %v\n", prefix, k, v)
	}
	probeAcc = map[string]time.Duration{}
	probeMu.Unlock()
}
