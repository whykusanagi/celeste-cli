package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ruling 30: every installed updater depends on these names. Renaming one in
// release.yml must fail here first.
func TestReleaseWorkflowKeepsTheUpdaterContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := strings.ReplaceAll(string(b), "\r\n", "\n") // a Windows checkout may convert line endings
	var want []string
	for _, p := range Platforms {
		want = append(want, "build "+p.GOOS+" "+p.GOARCH+" "+p.Binary+"\n") // Task 13's build helper
		if strings.HasSuffix(p.Archive, ".zip") {
			want = append(want, "zip "+p.Archive+" "+p.Binary)
		} else {
			want = append(want, "tar czf "+p.Archive+" "+p.Binary)
		}
	}
	want = append(want,
		"sha256sum *.tar.gz *.zip > checksums.txt",
		"--detach-sign --armor",
		"dist/checksums.txt\n",
		"dist/checksums.txt.asc\n",
		"dist/manifest.json\n",
		"dist/manifest.json.asc\n",
		"-X main.Channel=release",
		`update --verify-dist dist --tag "v${VERSION}"`,
	)
	for _, w := range want {
		if !strings.Contains(wf, w) {
			t.Errorf("release.yml no longer contains %q", w)
		}
	}
}
