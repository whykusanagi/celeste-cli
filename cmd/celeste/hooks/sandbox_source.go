package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// KindRepoSandbox is the "sandbox" object of a workspace's
// .celeste/config.json (2.0 W4). It runs nothing, but its loosening
// ("enabled": false, "network": true, extra writable paths) lets every
// bash command write or connect where the sandbox would stop it, so it is
// trusted like a repo hook: by content hash, in the same store, with the
// same `celeste hooks trust` flow.
const KindRepoSandbox SourceKind = "repo-sandbox"

// sandboxSuffix keeps a config file's sandbox settings apart from any
// other trust entry for the same path.
const sandboxSuffix = "#sandbox"

// SandboxSource is the trust source for one workspace config's "sandbox"
// object: keyed by the file's path plus "#sandbox", hashed over body (the
// object's canonical JSON), so any edit asks again.
func SandboxSource(configPath, body string) Source {
	sum := sha256.Sum256([]byte(body))
	return Source{
		Path:  configPath + sandboxSuffix,
		Root:  filepath.Dir(filepath.Dir(configPath)),
		Kind:  KindRepoSandbox,
		Rules: body,
		Hash:  hex.EncodeToString(sum[:]),
	}
}

// CheckRepoSandbox refuses a workspace config that is a symlink or sits
// in a symlinked .celeste directory, as repo hook files are refused: such
// a file is never trusted or asked about.
func CheckRepoSandbox(configPath string) error {
	info, err := os.Lstat(configPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing a symlinked .celeste/config.json; copy the file instead")
	}
	return refuseSymlinkedRepoComponents(configPath, filepath.Dir(filepath.Dir(configPath)))
}

// SourceFile is the file a trust source was read from: its path without
// the "#stream-rules" or "#sandbox" suffix that keeps it apart from other
// trust entries for the same file.
func SourceFile(src Source) string {
	switch src.Kind {
	case KindRepoStreamRules:
		return strings.TrimSuffix(src.Path, streamRulesSuffix)
	case KindRepoSandbox:
		return strings.TrimSuffix(src.Path, sandboxSuffix)
	}
	return src.Path
}
