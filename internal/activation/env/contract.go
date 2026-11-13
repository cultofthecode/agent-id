// Package env is the env slice: agent-id env --profile <name> --shell
// bash|zsh prints the child environment of the exec spec as one shell block
// that succeeds or fails as a whole (spec
// .ai/specs/2026-11-13-env-claude-session-start.md, #26). It names no forge
// and imports no other slice.
package env

import "errors"

// Input is one env request.
type Input struct {
	Profile string
	Shell   string
	// Dir is the working directory, for the remote checks.
	Dir string
	// Parent is the environment of the invoking process.
	Parent []string
}

// ExitFailure is the exit code of every failure, as exec uses it.
const ExitFailure = 125

// Errors of the slice.
var (
	ErrProfileRequired = errors.New("--profile is required")
	ErrShellRequired   = errors.New("--shell is required; use bash or zsh")
)

// ExitError carries the exit code of a failure. The composition root maps it
// to the process exit code.
type ExitError struct{ Err error }

// Error returns the message, which starts with "env: ".
func (e *ExitError) Error() string { return "env: " + e.Err.Error() }

// Unwrap returns the cause.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code.
func (e *ExitError) ExitCode() int { return ExitFailure }

func failure(err error) error { return &ExitError{Err: err} }
