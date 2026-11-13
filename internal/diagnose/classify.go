package diagnose

import (
	"errors"
	"fmt"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// ClassifyError returns the Failure of err: the closed class of the JSON
// contract and the hint. The hint of a provider class is fixed text; the
// hint of key, config, and cache is the error's own message, which names the
// path and the fix and never holds a secret. ErrRateLimited is matched
// before ErrUnavailable, which it wraps.
func ClassifyError(err error) Failure {
	switch {
	case errors.Is(err, provider.ErrRateLimited):
		return Failure{Class: ClassRateLimited, Message: "the host is rate limiting the agent; try again later"}
	case errors.Is(err, provider.ErrUnauthorized):
		return Failure{Class: ClassUnauthorized, Message: "the host rejected the App's credentials"}
	case errors.Is(err, provider.ErrForbidden):
		return Failure{Class: ClassForbidden, Message: "the host denied access"}
	case errors.Is(err, provider.ErrNotFound):
		return Failure{Class: ClassNotFound, Message: "the installation does not exist or the App lost access"}
	case errors.Is(err, provider.ErrUnavailable):
		return Failure{Class: ClassUnavailable, Message: "cannot reach the host"}
	case isKeyError(err):
		return Failure{Class: ClassKey, Message: err.Error()}
	case isCacheError(err):
		return Failure{Class: ClassCache, Message: err.Error()}
	}
	return Failure{Class: ClassConfig, Message: err.Error()}
}

// IsKeyError reports whether err is one of the credential source's errors.
func IsKeyError(err error) bool { return isKeyError(err) }

func isKeyError(err error) bool {
	for _, sentinel := range []error{credential.ErrInvalidReference, credential.ErrKeyNotFound, credential.ErrKeyUnreadable, credential.ErrInsecureKeyFile, credential.ErrInvalidKey} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

func isCacheError(err error) bool {
	for _, sentinel := range []error{token.ErrInsecureCache, token.ErrNoCacheDir, token.ErrLockTimeout, token.ErrCacheWrite, token.ErrUnusableToken, token.ErrInvalidKey} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// UsageError returns the exit-2 error for a flag or argument error. The text
// passes Redact and Guard, because cobra's usage errors echo flag values.
func UsageError(p Patterns, home string, err error) *ExitError {
	o := Output{Patterns: p, Home: home}
	return o.errorf(ExitUsage, "%v", err)
}

// FailureError returns the exit-1 error for an unexpected failure of a
// diagnostic command, with its text redacted and guarded.
func (o Output) FailureError(err error) *ExitError {
	return o.errorf(ExitFailure, "%v", err)
}

// WithheldError is the exit-3 error for output that Guard withheld.
func WithheldError() *ExitError {
	return &ExitError{Code: ExitWithheld, Message: WithheldMessage}
}

func (o Output) errorf(code int, format string, args ...any) *ExitError {
	text := o.sanitize(fmt.Sprintf(format, args...))
	if err := o.Patterns.Guard([]byte(text)); err != nil {
		return WithheldError()
	}
	return &ExitError{Code: code, Message: text}
}
