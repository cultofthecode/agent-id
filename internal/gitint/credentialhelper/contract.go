// Package credentialhelper is the slice behind `agent-id git-credential`: the
// Git credential helper of an agent context (spec
// .ai/specs/2026-11-13-git-credential-helper.md, #22). It answers `get` only
// for the profile's host over HTTPS, prints nothing for any other host, never
// persists a credential, and fails closed: when the token cannot be obtained
// Git gets `quit=1` and no credential.
package credentialhelper

import (
	"context"
	"errors"
	"io"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Operations of the Git credential helper protocol.
const (
	OpGet   = "get"
	OpStore = "store"
	OpErase = "erase"
)

// ErrUsage is returned for an unknown operation.
var ErrUsage = errors.New("git-credential: unknown operation")

// ExitFailure is the exit status of every failure.
const ExitFailure = 1

// Input is one helper call.
type Input struct {
	// Profile is the name from --profile.
	Profile string
	// Operation is get, store, or erase.
	Operation string
	// Stdin is Git's request.
	Stdin io.Reader
}

// Output is what the command writes to standard output. Stdout holds a
// secret after a successful get and is empty or `quit=1` otherwise.
type Output struct {
	Stdout string
}

// ClassedError labels a failure of Credential with one fixed word, so that
// the message never needs the error's text. Class is one of the words of
// the spec ("cache not private", "lock timeout", and so on).
type ClassedError struct {
	Class string
	Err   error
}

// Error returns the class word, never the text of the cause.
func (e *ClassedError) Error() string { return e.Class }

// Unwrap returns the cause, for errors.Is and errors.As.
func (e *ClassedError) Unwrap() error { return e.Err }

// Handler runs the operations of the helper. Both functions are injected by
// the composition root.
type Handler struct {
	// Host returns the provider host of the profile. It reads no key and
	// makes no request.
	Host func(profile string) (string, error)
	// Credential returns the Git credential of the profile, from the token
	// cache or newly minted. It loads the key. It is called only after the
	// decision allowed the request.
	Credential func(ctx context.Context, profile string) (provider.GitCredential, error)
}
