package main

import (
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/selfupdate"
)

const wantModule = "github.com/whykusanagi/celeste-cli/v2"

func TestModulePathIsV2(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	if first != "module "+wantModule {
		t.Fatalf("go.mod: %q, want %q", first, "module "+wantModule)
	}
}

// Every go install line and -X flag in the docs and build files uses the
// module path in go.mod.
func TestInstallLinesUseTheModulePath(t *testing.T) {
	old := regexp.MustCompile(`github\.com/whykusanagi/celeste-cli/cmd/`)
	for _, f := range []string{"../../README.md", "../../scripts/README.md", "../../Makefile", "../../.github/workflows/release.yml", "../../MIGRATING-2.0.md"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if loc := old.FindIndex(data); loc != nil {
			t.Errorf("%s still uses the pre-/v2 path near %q", f, data[loc[0]:min(len(data), loc[1]+30)])
		}
	}
}

// A go install of the module in go.mod is a Module build (it upgrades to
// the official release); look-alike paths stay Source. selfupdate.ModulePath
// keeps the base path and accepts its /vN major versions.
func TestGoInstallOfThisModuleIsAModuleBuild(t *testing.T) {
	for path, want := range map[string]selfupdate.Kind{
		wantModule:                                   selfupdate.Module,
		wantModule + "x":                             selfupdate.Source,
		wantModule + "/extra":                        selfupdate.Source,
		"github.com/someone/celeste-cli/v2":          selfupdate.Source,
		"github.com/whykusanagi/celeste-cli-fork/v2": selfupdate.Source,
	} {
		bi := &debug.BuildInfo{Main: debug.Module{Path: path, Version: "v2.0.0"}}
		if kind, _ := selfupdate.Classify(bi, true, false, false); kind != want {
			t.Errorf("Classify(%q) = %v, want %v", path, kind, want)
		}
	}
}
