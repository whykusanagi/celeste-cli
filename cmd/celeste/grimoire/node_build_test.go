package grimoire

import (
	"path/filepath"
	"testing"
)

// npm run build fails without a build script: it is named only when
// package.json defines one.
func TestNodeBuildCommandNeedsABuildScript(t *testing.T) {
	dir := t.TempDir()
	mk(t, filepath.Join(dir, "package.json"), `{"name":"x","scripts":{"test":"jest"}}`)
	info, err := DetectProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.BuildCommand != "" {
		t.Errorf("BuildCommand = %q without a build script", info.BuildCommand)
	}

	dir2 := t.TempDir()
	mk(t, filepath.Join(dir2, "package.json"), `{"name":"x","scripts":{"build":"tsc"}}`)
	info, err = DetectProject(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if info.BuildCommand != "npm run build" {
		t.Errorf("BuildCommand = %q with a build script, want npm run build", info.BuildCommand)
	}
}
