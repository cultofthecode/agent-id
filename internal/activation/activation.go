// Package activation owns the child environment of an agent context: the
// plan that says what a child gets, the pure builder that applies it, the
// reading of an inherited context, and the checks that must pass before a
// command starts (exec spec .ai/specs/2026-11-13-exec-child-environment.md,
// #20, #68). It names no forge: variables to drop, hosts, and the neutral CLI
// come from provider.Activator.
//
// The neutral credential baseline makes the child unable to reach the
// developer's credentials through Git or the provider's CLI, whether or not
// the bot's own credentials exist yet. No token or key is ever placed in the
// environment.
package activation

import (
	"errors"
	"fmt"
)

// Names of the context variables (contract version 1).
const (
	VarContext   = "AGENT_ID_CONTEXT"
	VarProfile   = "AGENT_ID_ACTIVE_PROFILE"
	VarMechanism = "AGENT_ID_MECHANISM"
	VarBinary    = "AGENT_ID_BIN"
	VarGitBase   = "AGENT_ID_GIT_CONFIG_BASE"
	VarGitEnd    = "AGENT_ID_GIT_CONFIG_END"

	// ContextVersion is the contract version that this package reads and
	// writes.
	ContextVersion = "1"
	// Prefix starts every variable that agent-id owns.
	Prefix = "AGENT_ID_"
)

// Mechanisms that start an agent context.
const (
	MechanismExec = "exec"
	MechanismEnv  = "env"
)

// Errors of activation. Every message names a variable, a key section, a
// path, or a remote; none contains a credential value.
var (
	ErrNestedProfile       = errors.New("activation: nested start with a different profile")
	ErrContextInvalid      = errors.New("activation: the inherited agent context is invalid")
	ErrInheritedGitConfig  = errors.New("activation: the inherited Git process configuration is unsafe")
	ErrBinaryPath          = errors.New("activation: cannot determine the agent-id binary")
	ErrPlanUnsafe          = errors.New("activation: the activation plan is unsafe")
	ErrProfileNotReady     = errors.New("activation: the profile is not ready")
	ErrIdentityUnavailable = errors.New("activation: the bot identity is unavailable")
	ErrNeutralDir          = errors.New("activation: a neutral directory is unsafe")
	ErrSSHRemote           = errors.New("activation: a remote uses SSH")
	ErrSSHRewrite          = errors.New("activation: a rule rewrites HTTPS to SSH")
	ErrRemoteCredentials   = errors.New("activation: a remote URL carries a password")
	ErrRemoteHeader        = errors.New("activation: an extra HTTP header survives")
	ErrNetrc               = errors.New("activation: a netrc file has credentials")
	ErrRemoteCheck         = errors.New("activation: a remote cannot be checked")
	ErrCommandNotFound     = errors.New("activation: command not found")
	ErrCommandNotExec      = errors.New("activation: command cannot be executed")
)

// Plan is what a child gets, from the contributions of the modules.
type Plan struct {
	Profile   string
	Mechanism string
	Binary    string
	// Drop lists variables removed from the child. It must not be empty.
	Drop []string
	// Set lists variables set in the child, after Drop.
	Set map[string]string
	// GitConfig is appended, in order, to the process-scoped Git
	// configuration.
	GitConfig []GitConfigEntry
	// PathPrepend lists directories put first on PATH.
	PathPrepend []string
	// TokenPrefixes are the provider's token prefixes, for the secret guard.
	TokenPrefixes []string
}

// GitConfigEntry is one process-scoped Git configuration entry.
type GitConfigEntry struct{ Key, Value string }

// Context is the agent context a process inherited.
type Context struct {
	Version, Profile, Mechanism, Binary string
	GitConfigBase, GitConfigEnd         int
}

// Start is how a start relates to the inherited context.
type Start int

// Starts.
const (
	StartFresh Start = iota
	StartReenter
)

// detailed is an error whose text is its message, without the sentinel's
// text, and that matches its sentinel with errors.Is. It keeps the one-line
// messages of the exec spec free of package prefixes.
type detailed struct {
	kind  error
	msg   string
	cause error
}

func (e *detailed) Error() string        { return e.msg }
func (e *detailed) Is(target error) bool { return target == e.kind }
func (e *detailed) Unwrap() error        { return e.cause }

func failf(kind error, format string, args ...any) error {
	return &detailed{kind: kind, msg: fmt.Sprintf(format, args...)}
}
