package server

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
)

// fakeEnvs is a chatEnvs whose builds are counted and whose Envs are bare
// (a zero loop.Env closes cleanly) with a real FileTracker.
type fakeEnvs struct {
	*chatEnvs
	builds atomic.Int32
	mu     sync.Mutex
	closed map[*loop.Env]bool
	stamp  string
	clock  time.Time
}

func newFakeEnvs(t *testing.T) *fakeEnvs {
	f := &fakeEnvs{chatEnvs: newChatEnvs(), closed: map[*loop.Env]bool{}, clock: time.Unix(1_000_000, 0)}
	f.build = func(_ *config.Config, _ string, opts loop.SetupOptions) (*loop.Env, error) {
		f.builds.Add(1)
		opts.Warn("setup warning")
		return &loop.Env{Files: checkpoints.NewFileTracker()}, nil
	}
	f.closeEnv = func(e *loop.Env) {
		f.mu.Lock()
		f.closed[e] = true
		f.mu.Unlock()
	}
	f.chatEnvs.stamp = func(string) string { return f.stamp }
	f.now = func() time.Time { return f.clock }
	t.Cleanup(f.close)
	return f
}

func (f *fakeEnvs) isClosed(e *chatEnv) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed[e.env]
}

// use acquires and immediately releases, returning the entry and the
// warnings the call received.
func (f *fakeEnvs) use(t *testing.T, ws string) (*chatEnv, string) {
	t.Helper()
	var w warnSink
	e, err := f.acquire(&config.Config{}, ws, &w)
	if err != nil {
		t.Fatal(err)
	}
	f.release(e)
	return e, w.section()
}

func TestChatEnvsReusesOneEnvPerWorkspace(t *testing.T) {
	f := newFakeEnvs(t)
	ws := t.TempDir()
	a, warnA := f.use(t, ws)
	b, warnB := f.use(t, ws+string(filepath.Separator)+".") // same cleaned path
	if a != b || f.builds.Load() != 1 {
		t.Fatalf("builds = %d, same entry = %v", f.builds.Load(), a == b)
	}
	if !strings.Contains(warnA, "setup warning") || warnB != "" {
		t.Fatalf("Setup warnings must reach the building call only: %q / %q", warnA, warnB)
	}
	f.use(t, t.TempDir())
	if f.builds.Load() != 2 {
		t.Fatalf("a second workspace must build its own Env: builds = %d", f.builds.Load())
	}
}

// Review focus 1: concurrent first calls on one workspace share one build.
func TestChatEnvsSharesOneBuildBetweenConcurrentFirstCalls(t *testing.T) {
	f := newFakeEnvs(t)
	gate := make(chan struct{})
	inner := f.build
	f.build = func(c *config.Config, ws string, o loop.SetupOptions) (*loop.Env, error) {
		<-gate
		return inner(c, ws, o)
	}
	ws := t.TempDir()
	var wg sync.WaitGroup
	got := make([]*chatEnv, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var w warnSink
			e, err := f.acquire(&config.Config{}, ws, &w)
			if err != nil {
				t.Error(err)
				return
			}
			got[i] = e
			f.release(e)
		}()
	}
	time.Sleep(50 * time.Millisecond) // let every call reach the build
	close(gate)
	wg.Wait()
	if f.builds.Load() != 1 {
		t.Fatalf("builds = %d, want 1", f.builds.Load())
	}
	for _, e := range got[1:] {
		if e != got[0] {
			t.Fatal("calls got different Envs")
		}
	}
}

// Review focus 2: a cached Env is replaced when a config input changed, and
// after the TTL.
func TestChatEnvsRebuildsWhenInputsChange(t *testing.T) {
	f := newFakeEnvs(t)
	ws := t.TempDir()
	first, _ := f.use(t, ws)
	f.stamp = "permissions.json changed"
	second, warn := f.use(t, ws)
	if f.builds.Load() != 2 || first == second || !f.isClosed(first) {
		t.Fatalf("builds = %d; old closed = %v", f.builds.Load(), f.isClosed(first))
	}
	if !strings.Contains(warn, "setup warning") {
		t.Fatal("the rebuilding call must get the new Setup warnings")
	}
	f.clock = f.clock.Add(chatEnvTTL + time.Second)
	f.use(t, ws)
	if f.builds.Load() != 3 {
		t.Fatalf("TTL did not rebuild: builds = %d", f.builds.Load())
	}
}

// Review focus 2: a stale Env is replaced even while a call is using it.
// The running call keeps the old Env (closed when it finishes), and the new
// call gets a fresh one, so constantly overlapping calls can't pin old rules.
func TestChatEnvsReplacesAStaleEnvUnderARunningCall(t *testing.T) {
	f := newFakeEnvs(t)
	ws := t.TempDir()
	var w warnSink
	held, err := f.acquire(&config.Config{}, ws, &w)
	if err != nil {
		t.Fatal(err)
	}
	f.stamp = "permissions.json changed"
	joined, warn := f.use(t, ws)
	if joined == held || f.builds.Load() != 2 {
		t.Fatalf("stale Env reused under a running call: builds = %d", f.builds.Load())
	}
	if !strings.Contains(warn, "setup warning") {
		t.Fatal("the rebuilding call must get the new Setup warnings")
	}
	if f.isClosed(held) {
		t.Fatal("closed the Env a running call still uses")
	}
	f.release(held)
	if !f.isClosed(held) {
		t.Fatal("the replaced Env was not closed on its last release")
	}
	if next, _ := f.use(t, ws); next != joined || f.builds.Load() != 2 {
		t.Fatalf("the fresh Env is not the cached one: builds = %d", f.builds.Load())
	}
}

// invalidate retires a workspace's Env (celeste_index rebuild is about to
// delete its codegraph DB): idle closes now, busy on its last release, and
// the next call builds anew.
func TestChatEnvsInvalidate(t *testing.T) {
	f := newFakeEnvs(t)
	ws := t.TempDir()
	idle, _ := f.use(t, ws)
	f.invalidate(ws)
	if !f.isClosed(idle) {
		t.Fatal("invalidate left an idle Env open")
	}
	var w warnSink
	busy, err := f.acquire(&config.Config{}, ws, &w)
	if err != nil {
		t.Fatal(err)
	}
	if busy == idle || f.builds.Load() != 2 {
		t.Fatalf("no rebuild after invalidate: builds = %d", f.builds.Load())
	}
	f.invalidate(ws)
	if f.isClosed(busy) {
		t.Fatal("invalidate closed an Env in use")
	}
	f.release(busy)
	if !f.isClosed(busy) {
		t.Fatal("invalidated Env not closed on its last release")
	}
	f.invalidate(t.TempDir()) // unknown workspace: no-op
}

func TestChatEnvsEvictsLeastRecentlyUsed(t *testing.T) {
	f := newFakeEnvs(t)
	f.max = 2
	ws1, ws2, ws3 := t.TempDir(), t.TempDir(), t.TempDir()
	e1, _ := f.use(t, ws1)
	f.clock = f.clock.Add(time.Second)
	f.use(t, ws2)
	f.clock = f.clock.Add(time.Second)
	f.use(t, ws3) // evicts ws1, the least recently used
	if !f.isClosed(e1) {
		t.Fatal("evicted idle Env not closed")
	}
	f.use(t, ws3)
	if f.builds.Load() != 3 {
		t.Fatalf("builds = %d, want 3 (ws3 cached)", f.builds.Load())
	}
	f.use(t, ws1)
	if f.builds.Load() != 4 {
		t.Fatalf("builds = %d, want 4 (ws1 rebuilt)", f.builds.Load())
	}
}

func TestChatEnvsEvictionWaitsForInFlightCalls(t *testing.T) {
	f := newFakeEnvs(t)
	f.max = 1
	var w warnSink
	held, err := f.acquire(&config.Config{}, t.TempDir(), &w)
	if err != nil {
		t.Fatal(err)
	}
	f.use(t, t.TempDir()) // evicts held's entry while it runs
	if f.isClosed(held) {
		t.Fatal("closed an Env a call is still using")
	}
	f.release(held)
	if !f.isClosed(held) {
		t.Fatal("retired Env not closed on its last release")
	}
}

func TestChatEnvsCloseOnShutdown(t *testing.T) {
	f := newFakeEnvs(t)
	idle, _ := f.use(t, t.TempDir())
	var w warnSink
	busy, err := f.acquire(&config.Config{}, t.TempDir(), &w)
	if err != nil {
		t.Fatal(err)
	}
	f.close()
	if !f.isClosed(idle) || f.isClosed(busy) {
		t.Fatalf("idle closed = %v, busy closed = %v", f.isClosed(idle), f.isClosed(busy))
	}
	f.release(busy)
	if !f.isClosed(busy) {
		t.Fatal("busy Env not closed on release after shutdown")
	}
	if _, err := f.acquire(&config.Config{}, t.TempDir(), &w); err == nil {
		t.Fatal("acquire after close must fail")
	}
}

// Staleness is judged per call: a read from an earlier call does not make
// a later call's write fail. Overlapping calls share the tracker.
func TestChatEnvsResetsTheFileTrackerBetweenCalls(t *testing.T) {
	f := newFakeEnvs(t)
	ws := t.TempDir()
	path := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)

	var w1 warnSink
	e, _ := f.acquire(&config.Config{}, ws, &w1)
	_ = e.env.Files.RecordRead(path)
	var w2 warnSink
	overlap, _ := f.acquire(&config.Config{}, ws, &w2) // joins: no reset
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if overlap.env.Files.CheckStale(path) == nil {
		t.Fatal("an overlapping call must share the tracker")
	}
	f.release(overlap)
	f.release(e)

	next, _ := f.use(t, ws) // first call on an idle Env: reset
	if err := next.env.Files.CheckStale(path); err != nil {
		t.Fatalf("an earlier call's read leaked into this call: %v", err)
	}
}

// celeste_index rebuild retires the workspace's cached chat Env before it
// deletes the codegraph DB that Env holds open.
func TestIndexRebuildInvalidatesTheChatEnv(t *testing.T) {
	srv, dir := newTestServerWithWorkspace(t)
	var closed []*loop.Env
	srv.chatEnvs.build = func(*config.Config, string, loop.SetupOptions) (*loop.Env, error) { return &loop.Env{}, nil }
	srv.chatEnvs.closeEnv = func(e *loop.Env) { closed = append(closed, e) }
	var w warnSink
	e, err := srv.chatEnvs.acquire(&config.Config{}, dir, &w)
	if err != nil {
		t.Fatal(err)
	}
	srv.chatEnvs.release(e)
	writeTSFile(t, dir, "a.ts", "export function a(): number { return 1; }\n")
	callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	if len(closed) != 1 || closed[0] != e.env {
		t.Fatalf("rebuild did not retire the chat Env: closed %d", len(closed))
	}
}
