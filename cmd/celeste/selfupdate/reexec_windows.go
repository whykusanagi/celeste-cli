//go:build windows

package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// Reexec runs exe with argv's arguments and env on this console, waits, and
// exits with its code (ruling 26): Windows has no exec. It returns only if
// exe could not start.
func Reexec(exe string, argv, env []string) error {
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return err
	}
	// The console sends Ctrl+C to the child as well; the parent only waits.
	signal.Ignore(os.Interrupt)
	err := cmd.Wait()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		os.Exit(exit.ExitCode())
	case err != nil:
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}
