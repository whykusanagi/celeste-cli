package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/personacrypt/personacrypttest"
)

const testBoundary = "Voice Boundary: test rule."

func unsetEnv(t *testing.T, k string) {
	t.Helper()
	t.Setenv(k, "") // restores the old value after the test
	os.Unsetenv(k)
}

func fakeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, personaDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func writePlain(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for p, data := range personacrypttest.Plaintexts(testBoundary) {
		if err := os.WriteFile(filepath.Join(dir, "celeste_"+p+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sealArgs(repo, plain string) []string {
	return []string{"seal", "-repo", repo, "-in", plain, "-core", personacrypttest.CoreCommit, "-container", personacrypttest.ContainerCommit}
}

func TestPersonasealSealOpenCheck(t *testing.T) {
	t.Setenv("CELESTE_PERSONA_KEY", personacrypttest.Key)
	repo, plain := fakeRepo(t), writePlain(t)
	var out bytes.Buffer
	if err := run(sealArgs(repo, plain), &out); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(repo, personaDir)); len(entries) != 5 {
		t.Fatalf("wrote %d files, want 4 sealed files and SOURCE.json", len(entries))
	}
	if err := run([]string{"check", "-repo", repo, "-in", plain}, &out); err != nil {
		t.Fatalf("check right after seal: %v", err)
	}
	opened := t.TempDir()
	if err := run([]string{"open", "-repo", repo, "-out", opened}, &out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(opened, "celeste_full.json"))
	want, _ := os.ReadFile(filepath.Join(plain, "celeste_full.json"))
	if !bytes.Equal(got, want) {
		t.Fatal("open did not return the sealed plaintext")
	}
	if strings.Contains(strings.ToLower(out.String()), personacrypttest.Key) {
		t.Fatal("the key was printed")
	}
	if err := os.WriteFile(filepath.Join(plain, "celeste_lite.json"), []byte(`{"profile":"lite","system_prompt":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-repo", repo, "-in", plain}, &out); err == nil || !strings.Contains(err.Error(), "lite") {
		t.Fatalf("check missed a changed lite: %v", err)
	}
}

// Plaintext never lands in the repository tree (ruling 3).
func TestPersonasealOpenRefusesTheRepoTree(t *testing.T) {
	t.Setenv("CELESTE_PERSONA_KEY", personacrypttest.Key)
	repo, plain := fakeRepo(t), writePlain(t)
	if err := run(sealArgs(repo, plain), io.Discard); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "scratch")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"open", "-repo", repo, "-out", inside}, io.Discard); err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(inside); len(entries) != 0 {
		t.Fatal("plaintext was written inside the repository")
	}
}

func TestPersonasealReadKeyRefusesAGroupReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}
	unsetEnv(t, "CELESTE_PERSONA_KEY")
	path := filepath.Join(t.TempDir(), "persona.key")
	if err := os.WriteFile(path, []byte(personacrypttest.Key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELESTE_PERSONA_KEY_FILE", path)
	if _, err := readKey(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("err = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKey(); err != nil {
		t.Fatal(err)
	}
}

// Review Focus 6: a bad key, from the environment or the file, is never
// quoted back.
func TestPersonasealKeyErrorsNeverQuoteTheKey(t *testing.T) {
	bad := personacrypttest.Key[:63]
	path := filepath.Join(t.TempDir(), "persona.key")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	unsetEnv(t, "CELESTE_PERSONA_KEY")
	t.Setenv("CELESTE_PERSONA_KEY_FILE", path)
	if _, err := readKey(); err == nil || strings.Contains(err.Error(), bad[:16]) {
		t.Fatalf("file: err = %v", err)
	}
	t.Setenv("CELESTE_PERSONA_KEY", bad)
	if _, err := readKey(); err == nil || strings.Contains(err.Error(), bad[:16]) {
		t.Fatalf("env: err = %v", err)
	}
}
