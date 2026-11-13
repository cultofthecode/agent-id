//go:build !darwin && !linux

package exec

import "errors"

// SyscallExecer is not supported on this platform.
type SyscallExecer struct{}

// Exec implements Execer.
func (SyscallExecer) Exec(string, []string, []string) error {
	return errors.New("exec is supported on macOS and Linux only")
}
