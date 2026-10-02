//go:build !windows

package selfupdate

import "syscall"

// Reexec replaces this process with exe, keeping argv and env (ruling 26).
// It returns only on failure.
func Reexec(exe string, argv, env []string) error {
	return syscall.Exec(exe, argv, env)
}
