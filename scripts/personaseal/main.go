// Command personaseal seals Celeste's persona profiles into celeste-cli and
// opens them for local work (W5). It never prints the key.
//
//	go run ./scripts/personaseal seal  -in <dir> -core <sha> -container <sha>
//	go run ./scripts/personaseal open  -out <existing dir outside the repo>
//	go run ./scripts/personaseal check -in <dir>
//
// -in holds a container build's celeste_<profile>.json files. The key comes
// from CELESTE_PERSONA_KEY, else the file CELESTE_PERSONA_KEY_FILE, else
// ~/.celeste/persona.key, which group and others must not be able to read.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/personacrypt"
)

// personaDir is where the sealed persona lives, relative to the repository.
const personaDir = "cmd/celeste/prompts/persona"

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "personaseal:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: personaseal seal|open|check [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	in := flags.String("in", "", "directory holding celeste_<profile>.json (seal, check)")
	outDir := flags.String("out", "", "existing directory outside the repository to decrypt into (open)")
	core := flags.String("core", "", "the core-persona commit the build is from (seal)")
	container := flags.String("container", "", "the persona-container commit (seal)")
	repo := flags.String("repo", ".", "the celeste-cli checkout")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	key, err := readKey()
	if err != nil {
		return err
	}
	dir := filepath.Join(*repo, personaDir)
	switch args[0] {
	case "seal":
		return seal(key, dir, *in, *core, *container, out)
	case "open":
		return open(key, dir, *repo, *outDir, out)
	case "check":
		return check(key, dir, *in, out)
	}
	return fmt.Errorf("unknown command %q (seal, open or check)", args[0])
}

// readKey loads the persona key. Errors say where the key was looked for,
// never what was in it (ruling 15).
func readKey() ([]byte, error) {
	if v, ok := os.LookupEnv("CELESTE_PERSONA_KEY"); ok {
		key, err := personacrypt.ParseKey(v)
		if err != nil {
			return nil, fmt.Errorf("CELESTE_PERSONA_KEY: %w", err)
		}
		return key, nil
	}
	path := os.Getenv("CELESTE_PERSONA_KEY_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".celeste", "persona.key")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no persona key: set CELESTE_PERSONA_KEY or create %s with mode 0600", displayPath(path))
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s can be read by group or others (mode %o); run chmod 600 on it", displayPath(path), info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", displayPath(path), err)
	}
	key, err := personacrypt.ParseKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", displayPath(path), err)
	}
	return key, nil
}

// displayPath shows a path under the home directory as ~/…, so output
// never carries a machine's user directory.
func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return p
}

// readPlain reads a build's four profiles.
func readPlain(in string) (map[string][]byte, error) {
	if in == "" {
		return nil, errors.New("-in is required")
	}
	plain := make(map[string][]byte, len(personacrypt.Profiles))
	for _, p := range personacrypt.Profiles {
		data, err := os.ReadFile(filepath.Join(in, "celeste_"+p+".json"))
		if err != nil {
			return nil, fmt.Errorf("the build has no %s profile: %w", p, err)
		}
		plain[p] = data
	}
	return plain, nil
}

// readCommitted is the committed SOURCE.json and sealed files; nil when
// there are none yet or SOURCE.json doesn't parse.
func readCommitted(dir string) (*personacrypt.Source, map[string][]byte) {
	data, err := os.ReadFile(filepath.Join(dir, "SOURCE.json"))
	if err != nil {
		return nil, nil
	}
	src, err := personacrypt.ParseSource(data)
	if err != nil {
		return nil, nil
	}
	files := map[string][]byte{}
	for _, e := range src.Profiles {
		if b, err := os.ReadFile(filepath.Join(dir, e.File)); err == nil {
			files[e.File] = b
		}
	}
	return src, files
}

func seal(key []byte, dir, in, core, container string, out io.Writer) error {
	if !fullSHA.MatchString(core) || !fullSHA.MatchString(container) {
		return errors.New("seal needs full 40-character -core and -container commits")
	}
	plain, err := readPlain(in)
	if err != nil {
		return err
	}
	prev, prevFiles := readCommitted(dir)
	src, files, err := personacrypt.SealSet(key, core, container, plain, prev, prevFiles)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range src.Profiles {
		if err := os.WriteFile(filepath.Join(dir, e.File), files[e.File], 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "%-6s %8d bytes sealed  ~%d tokens\n", e.Profile, e.CiphertextBytes, e.ApproxTokens)
	}
	return os.WriteFile(filepath.Join(dir, "SOURCE.json"), src.JSON(), 0o644)
}

// open decrypts the committed profiles into outDir for local reading (make
// persona-dev). outDir must already exist outside the repository: plaintext
// never goes in the repo tree (ruling 3).
func open(key []byte, dir, repo, outDir string, out io.Writer) error {
	if outDir == "" {
		return errors.New("-out is required")
	}
	inside, err := within(repo, outDir)
	if err != nil {
		return err
	}
	if inside {
		return errors.New("-out is inside the repository; decrypted persona files never go in the repository tree")
	}
	src, files := readCommitted(dir)
	if src == nil {
		return errors.New("no committed persona (run make sync-persona)")
	}
	if personacrypt.KeyID(key) != src.KeyID {
		return personacrypt.ErrWrongKey
	}
	for _, e := range src.Profiles {
		pt, err := personacrypt.Open(key, files[e.File], personacrypt.AAD(e.Profile, src.CoreCommit))
		if err != nil {
			return fmt.Errorf("%s: %w", e.Profile, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, "celeste_"+e.Profile+".json"), pt, 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "decrypted %d profiles\n", len(src.Profiles))
	return nil
}

// check compares a fresh build (in) with what is committed: each profile's
// plaintext hash must equal SOURCE.json's, and the committed ciphertext must
// open to those same bytes (make persona-check).
func check(key []byte, dir, in string, out io.Writer) error {
	plain, err := readPlain(in)
	if err != nil {
		return err
	}
	src, files := readCommitted(dir)
	if src == nil {
		return errors.New("no committed persona (run make sync-persona)")
	}
	if personacrypt.KeyID(key) != src.KeyID {
		return personacrypt.ErrWrongKey
	}
	var problems []string
	for _, e := range src.Profiles {
		pt := plain[e.Profile]
		if personacrypt.SHA256Hex(pt) != e.PlaintextSHA256 {
			problems = append(problems, e.Profile+": the rebuild differs from SOURCE.json's plaintext_sha256 (run make sync-persona)")
			continue
		}
		got, err := personacrypt.Open(key, files[e.File], personacrypt.AAD(e.Profile, src.CoreCommit))
		if err != nil || !bytes.Equal(got, pt) {
			problems = append(problems, e.Profile+": the committed ciphertext does not open to the rebuild")
		}
	}
	if len(problems) > 0 {
		return errors.New("persona drift:\n  " + strings.Join(problems, "\n  "))
	}
	fmt.Fprintf(out, "OK: the committed persona matches a rebuild at %.12s / %.12s\n", src.CoreCommit, src.ContainerCommit)
	return nil
}

// within reports whether path is inside root, after resolving symlinks (on
// macOS a temp directory is reached through /var → /private/var). path must
// exist.
func within(root, path string) (bool, error) {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, errors.New("-out must be an existing directory")
	}
	if r, err = filepath.Abs(r); err != nil {
		return false, err
	}
	if p, err = filepath.Abs(p); err != nil {
		return false, err
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false, nil // another volume: outside
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}
