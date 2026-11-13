// Package exec is the exec slice: agent-id exec --profile <name> [--]
// <command> [args…] starts the command, and only its process tree, in the
// agent context of the profile (exec spec,
// .ai/specs/2026-11-13-exec-child-environment.md, #20). The process replaces
// itself with the command, so the exit status and every signal are the
// command's. It names no forge and imports no other slice.
package exec

import "errors"

// Input is one exec request.
type Input struct {
	Profile string
	// Argv is the command and its arguments.
	Argv []string
	// Dir is the working directory, for the remote checks.
	Dir string
	// Parent is the environment of the invoking process.
	Parent []string
}

// Exit codes of agent-id's own failures, as env(1) uses them.
const (
	ExitFailure       = 125
	ExitNotExecutable = 126
	ExitNotFound      = 127
)

// Errors of the slice.
var (
	ErrNoCommand       = errors.New("no command given; usage: agent-id exec --profile <name> [--] <command> [args…]")
	ErrProfileRequired = errors.New("--profile is required")
)

// ExitError carries the exit code of a failure that happened before the
// command started. The composition root maps it to the process exit code.
type ExitError struct {
	Code int
	Err  error
}

// Error returns the message, which starts with "exec: ".
func (e *ExitError) Error() string { return "exec: " + e.Err.Error() }

// Unwrap returns the cause.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code.
func (e *ExitError) ExitCode() int { return e.Code }

func failure(code int, err error) error { return &ExitError{Code: code, Err: err} }
