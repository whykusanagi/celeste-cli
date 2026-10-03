package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxSourceKeyAndHash(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".celeste", "config.json")
	a := SandboxSource(p, `{"enabled":false}`)
	b := SandboxSource(p, `{"enabled":false,"writable":["/x"]}`)
	if a.Path != p+"#sandbox" || a.Kind != KindRepoSandbox || a.Global() {
		t.Fatalf("source = %+v", a)
	}
	if a.Hash == "" || a.Hash == b.Hash {
		t.Fatal("the hash covers the body")
	}
	if a.Root != filepath.Dir(filepath.Dir(p)) {
		t.Fatalf("Root = %s", a.Root)
	}
	var out bytes.Buffer
	DescribeSource(&out, b)
	if !strings.Contains(out.String(), `writable`) {
		t.Fatalf("DescribeSource = %q", out.String())
	}
	out.Reset()
	PromptApprover(strings.NewReader("y\n"), &out)(b, Untrusted)
	if !strings.Contains(out.String(), "Sandbox settings") {
		t.Fatalf("prompt = %q", out.String())
	}
}

func TestCheckRepoSandboxRefusesSymlinks(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".celeste")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(ws, "real.json")
	if err := os.WriteFile(real, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.json")
	if err := os.Symlink(real, p); err != nil {
		t.Skip(err)
	}
	if err := CheckRepoSandbox(p); err == nil {
		t.Fatal("a symlinked config.json is refused")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepoSandbox(p); err != nil {
		t.Fatalf("a regular file: %v", err)
	}
	ws2 := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(ws2, ".celeste")); err != nil {
		t.Skip(err)
	}
	if err := CheckRepoSandbox(filepath.Join(ws2, ".celeste", "config.json")); err == nil {
		t.Fatal("a symlinked .celeste directory is refused")
	}
}

func TestSourceFileStripsTheTrustSuffix(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".celeste", "config.json")
	g := filepath.Join(t.TempDir(), ".grimoire")
	for _, tc := range []struct {
		src  Source
		want string
	}{
		{SandboxSource(p, `{"enabled":false}`), p},
		{Source{Path: g + streamRulesSuffix, Kind: KindRepoStreamRules}, g},
		{Source{Path: p, Kind: KindRepo}, p},
	} {
		if got := SourceFile(tc.src); got != tc.want {
			t.Errorf("SourceFile(%q) = %q, want %q", tc.src.Path, got, tc.want)
		}
	}
}
