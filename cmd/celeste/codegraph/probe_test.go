package codegraph

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestProbe385 times the phases of Go index builds (throwaway, #385).
func TestProbe385(t *testing.T) {
	probeOn = true
	t.Logf("runtime.GOROOT=%q GOOS=%s", runtime.GOROOT(), runtime.GOOS)
	goBin, err := exec.LookPath("go")
	t.Logf("go=%q err=%v", goBin, err)
	run := func(label, dir string, env []string, args ...string) {
		start := time.Now()
		cmd := exec.Command(goBin, args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		s := string(out)
		if len(s) > 300 {
			s = s[:300]
		}
		t.Logf("PROBE %-34s %v err=%v out=%q", label, time.Since(start), err, strings.TrimSpace(s))
	}
	base := os.Environ()
	tmpHome := t.TempDir()
	swapped := append(append([]string{}, base...), "HOME="+tmpHome, "USERPROFILE="+tmpHome)
	for i := 0; i < 2; i++ {
		run(fmt.Sprintf("go version #%d", i), "", base, "version")
		run(fmt.Sprintf("go env GOROOT #%d", i), "", append(base, "GOTOOLCHAIN=local"), "env", "GOROOT")
		run(fmt.Sprintf("go env GOROOT swappedHOME #%d", i), "", append(swapped, "GOTOOLCHAIN=local"), "env", "GOROOT")
		run(fmt.Sprintf("go env GOCACHE GOMODCACHE swapped #%d", i), "", swapped, "env", "GOCACHE", "GOMODCACHE", "GOFLAGS", "GOPATH")
	}

	// Module fixture.
	mod := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644))
	must(os.MkdirAll(filepath.Join(mod, "a"), 0o755))
	must(os.WriteFile(filepath.Join(mod, "a", "a.go"), []byte("package a\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc A() string { return fmt.Sprint(strings.ToUpper(\"x\")) }\n"), 0o644))
	must(os.WriteFile(filepath.Join(mod, "a", "a_test.go"), []byte("package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { A() }\n"), 0o644))
	must(os.WriteFile(filepath.Join(mod, "main.go"), []byte("package main\n\nimport \"example.com/m/a\"\n\nfunc main() { a.A() }\n"), 0o644))
	env := append(append([]string{}, base...), "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	for i := 0; i < 2; i++ {
		run(fmt.Sprintf("go list -deps -test #%d", i), mod, env, "list", "-mod=readonly", "-e", "-deps", "-test", "-json=ImportPath,Dir,GoFiles,ImportMap", "./...")
		run(fmt.Sprintf("go list -deps (no -test) #%d", i), mod, env, "list", "-mod=readonly", "-e", "-deps", "-json=ImportPath,Dir,GoFiles,ImportMap", "./...")
		run(fmt.Sprintf("go list -find ./... #%d", i), mod, env, "list", "-mod=readonly", "-e", "-find", "./...")
		run(fmt.Sprintf("go list -deps swappedHOME #%d", i), mod, append(append([]string{}, swapped...), "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0"), "list", "-mod=readonly", "-e", "-deps", "-test", "-json=ImportPath,Dir,GoFiles,ImportMap", "./...")
	}

	build := func(label, ws string) {
		idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "i.db"))
		must(err)
		defer idx.Close()
		start := time.Now()
		must(idx.Build())
		t.Logf("PROBE Build %-28s %v", label, time.Since(start))
		start = time.Now()
		must(idx.Update())
		t.Logf("PROBE Update(noop) %-21s %v", label, time.Since(start))
	}
	for i := 0; i < 2; i++ {
		build(fmt.Sprintf("module #%d", i), mod)
	}

	// The TestCodeSearchHonoursTopK fixture: no go.mod, no imports.
	plain := t.TempDir()
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {}\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "func validateSessionToken%d(token string) bool { return helper(token) }\n", i)
	}
	b.WriteString("func helper(s string) bool { return s != \"\" }\n")
	must(os.WriteFile(filepath.Join(plain, "main.go"), []byte(b.String()), 0o644))
	for i := 0; i < 2; i++ {
		build(fmt.Sprintf("no-gomod #%d", i), plain)
	}
	// Same with HOME/USERPROFILE swapped like the server tests do.
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	for i := 0; i < 2; i++ {
		build(fmt.Sprintf("no-gomod swappedHOME #%d", i), plain)
		build(fmt.Sprintf("module swappedHOME #%d", i), mod)
	}
	// Non-Go control.
	py := t.TempDir()
	must(os.WriteFile(filepath.Join(py, "a.py"), []byte("def f():\n    return g()\n\ndef g():\n    return 1\n"), 0o644))
	build("python control", py)
}
