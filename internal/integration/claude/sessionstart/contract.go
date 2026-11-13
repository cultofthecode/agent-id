// Package sessionstart is the slice of agent-id claude-code session-start:
// a Claude Code SessionStart command hook that appends the guarded
// activation block of a profile to CLAUDE_ENV_FILE once, or a refusal line
// when activation fails, so that every Bash command of the session either
// starts in agent mode or does not start (spec
// .ai/specs/2026-11-13-env-claude-session-start.md, #26). It imports
// internal/activation and the pure functions of internal/integration/claude,
// names no forge, and imports no other slice.
package sessionstart

import "errors"

// MaxHookInput is the size limit of the hook input, in bytes.
const MaxHookInput = 64 << 10

// Exit codes of session-start. They differ from cobra's 1, so the documented
// hook command can tell agent-id's own outcome from a usage error, an older
// agent-id without the command, a missing binary, or a crash.
const (
	ExitRefused = 3
	ExitNoFile  = 4
)

// Input is one session-start request.
type Input struct {
	Profile string
	// HookInput is the JSON that Claude Code writes to the hook's stdin,
	// read by the command up to MaxHookInput+1 bytes.
	HookInput []byte
	// EnvFile is the value of CLAUDE_ENV_FILE.
	EnvFile string
	// Parent is the environment of the hook process.
	Parent []string
}

// Errors of the slice.
var (
	ErrNotAHook = errors.New("must run as a Claude Code SessionStart command hook (CLAUDE_ENV_FILE is not set)")
)

// ExitError carries the exit code of an outcome that is not success.
type ExitError struct {
	Code int
	Err  error
}

// Error returns the message, which starts with "claude-code session-start: ".
func (e *ExitError) Error() string { return "claude-code session-start: " + e.Err.Error() }

// Unwrap returns the cause.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code.
func (e *ExitError) ExitCode() int { return e.Code }

func noFile(err error) error  { return &ExitError{Code: ExitNoFile, Err: err} }
func refused(err error) error { return &ExitError{Code: ExitRefused, Err: err} }
