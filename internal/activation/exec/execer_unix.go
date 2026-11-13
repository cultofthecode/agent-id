//go:build darwin || linux

package exec

import "syscall"

// SyscallExecer replaces the process with execve.
type SyscallExecer struct{}

// Exec implements Execer.
func (SyscallExecer) Exec(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
