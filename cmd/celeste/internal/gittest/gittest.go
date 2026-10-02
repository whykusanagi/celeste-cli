// Package gittest runs git in tests without ever touching a repository the
// test did not create: variables a parent git may export (git rebase -x
// exports GIT_DIR) are dropped, and the identity is passed per command, so
// no test writes user.name or user.email into any config.
package gittest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Name and Email are the test identity.
const (
	Name  = "test"
	Email = "test@example.invalid"
)

// leaked are the variables that point git at another repository.
var leaked = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES", "GIT_PREFIX",
}

// identity is set for every git a test runs, directly or through the code
// under test. GIT_CONFIG_COUNT/KEY/VALUE give user.name and user.email as
// configuration (git config user.name reads them) without writing a file,
// and turn off commit signing a developer's global config may ask for.
var identity = []string{
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_COUNT=3",
	"GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=" + Name,
	"GIT_CONFIG_KEY_1=user.email", "GIT_CONFIG_VALUE_1=" + Email,
	"GIT_CONFIG_KEY_2=commit.gpgsign", "GIT_CONFIG_VALUE_2=false",
	"GIT_AUTHOR_NAME=" + Name, "GIT_AUTHOR_EMAIL=" + Email,
	"GIT_COMMITTER_NAME=" + Name, "GIT_COMMITTER_EMAIL=" + Email,
}

// Env is os.Environ without the leaked variables, with the test identity.
func Env() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !dropped(k) {
			out = append(out, kv)
		}
	}
	return append(out, identity...)
}

// dropped reports whether Env and Isolate remove k: a leaked variable, an
// inherited identity, or inherited configuration (a parent's git -c exports
// GIT_CONFIG_PARAMETERS).
func dropped(k string) bool {
	for _, l := range leaked {
		if k == l {
			return true
		}
	}
	return k == "GIT_CONFIG" || strings.HasPrefix(k, "GIT_AUTHOR_") ||
		strings.HasPrefix(k, "GIT_COMMITTER_") || strings.HasPrefix(k, "GIT_CONFIG_")
}

// Isolate applies Env to the test process, so git run by the code under
// test is isolated too. Call it from TestMain.
func Isolate() {
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); dropped(k) {
			_ = os.Unsetenv(k)
		}
	}
	for _, kv := range identity {
		k, v, _ := strings.Cut(kv, "=")
		_ = os.Setenv(k, v)
	}
}

// Command is git args in dir, isolated, with the identity also given as
// -c options.
func Command(dir string, args ...string) *exec.Cmd {
	full := append([]string{"-c", "user.name=" + Name, "-c", "user.email=" + Email}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = Env()
	return cmd
}

// Run runs Command and fails the test on an error; it returns the output.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := Command(dir, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
