package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/atomicfile"
	"golang.org/x/term"
)

// jevNotice is printed by `celeste config --init jev` (spec §5 W3: the
// docs and the command state that excerpts go to a third party).
const jevNotice = `Jev (TypeSafe) is on in shadow mode for jev_prune, jev_gate and jev_route:
celeste asks Jev and logs what it would do; nothing acts until you set one
of them to "on" in this profile's config file.

Redacted excerpts are sent to TypeSafe, a third party: old tool results
(jev_prune), tool-call arguments and your request (jev_gate), and
/orchestrate goals (jev_route). Secrets that look like keys, tokens and
passwords are replaced with [REDACTED] first, and file paths are sent
relative to the workspace (or as <path>); redaction is best-effort.
Remove the key file to turn Jev off everywhere.`

// initJev is `celeste config --init jev`: it writes the TypeSafe key to
// ~/.celeste/typesafe.key (mode 0600) and sets jev_prune, jev_gate and
// jev_route to "shadow" in cfg, never lowering one already "on". The key
// comes from TYPESAFE_API_KEY, else one line of in; with neither, an
// existing key file is kept. The caller saves cfg.
func initJev(cfg *config.Config, home string, in io.Reader, out io.Writer) error {
	keyPath := filepath.Join(home, ".celeste", "typesafe.key")
	key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	if key == "" {
		fmt.Fprint(out, "TypeSafe API key (from https://typesafe.ai; Enter keeps the saved one): ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		key = strings.TrimSpace(line)
		fmt.Fprintln(out)
	}
	switch {
	case key != "":
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
			return err
		}
		// Atomic, so the key is never readable at an old file's mode.
		if err := atomicfile.Write(keyPath, []byte(key+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Fprintf(out, "Saved the key to %s (readable only by you)\n", keyPath)
	default:
		if _, err := os.Stat(keyPath); err != nil {
			return errors.New("no TypeSafe key given: set TYPESAFE_API_KEY or paste the key when asked")
		}
		fmt.Fprintf(out, "Keeping the key in %s\n", keyPath)
	}
	for _, f := range []*string{&cfg.JevPrune, &cfg.JevGate, &cfg.JevRoute} {
		if *f != config.ModeOn {
			*f = config.ModeShadow
		}
	}
	fmt.Fprintln(out, jevNotice)
	return nil
}

// keyInput is where `celeste config --init jev` reads a pasted key: from a
// terminal without echo, so the key never shows on screen or in
// scrollback; from a pipe as is.
func keyInput(f *os.File) io.Reader {
	if !term.IsTerminal(int(f.Fd())) {
		return f
	}
	return &silentLine{fd: int(f.Fd())}
}

// silentLine reads one line from a terminal with echo off, on first Read.
type silentLine struct {
	fd  int
	buf *strings.Reader
}

func (s *silentLine) Read(p []byte) (int, error) {
	if s.buf == nil {
		b, err := term.ReadPassword(s.fd)
		if err != nil {
			return 0, err
		}
		s.buf = strings.NewReader(string(b) + "\n")
	}
	return s.buf.Read(p)
}
