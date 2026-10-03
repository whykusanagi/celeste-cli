package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// Without a known home directory the key is not saved at all: a relative
// .celeste/typesafe.key would land in whatever directory celeste ran in.
func TestInitJevNeedsAnAbsoluteHome(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	t.Setenv("TYPESAFE_API_KEY", "ts-test-key")
	for _, home := range []string{"", "relative/home"} {
		if err := initJev(&config.Config{}, home, strings.NewReader(""), &bytes.Buffer{}); err == nil {
			t.Errorf("initJev with home %q succeeded", home)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".celeste")); err == nil {
		t.Error("a key directory was created in the working directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "relative")); err == nil {
		t.Error("a key directory was created under the relative home")
	}
}
