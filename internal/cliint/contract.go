// Package cliint runs the provider's known CLI (gh in the MVP) for an agent
// with a fresh token on each invocation (#23, spec
// .ai/specs/2026-11-13-gh-shim.md). The launcher script that
// internal/activation puts first on PATH starts it as
// "agent-id cli <binary> -- <args>". It checks the arguments against the
// provider's rules, takes a token from the token manager, builds the CLI's
// environment, and replaces itself with the real CLI, so the exit status and
// the signals are the CLI's. It names no forge: the binary, the variables,
// the host pin, and the rules come from provider.Activator and
// provider.Provider.
//
// The package fails closed. When any step fails, the CLI does not start, and
// nothing falls back to the developer's credentials. No token is printed,
// logged, or put in an error.
package cliint

import (
	"errors"
	"strings"
)

// ExitRefused is the exit code of every refusal. It is the code that exec
// uses for "agent-id failed before the command started".
const ExitRefused = 125

// VarActive marks the environment of a CLI that the shim started. A shim that
// finds it set refuses, so a CLI that starts "gh" through PATH cannot loop.
const VarActive = "AGENT_ID_CLI_ACTIVE"

// Input is one invocation of the shim.
type Input struct {
	// Binary is the CLI's command name, for example gh.
	Binary string
	// Args are the arguments of the CLI, as the agent typed them.
	Args []string
	// Parent is the environment of the shim process.
	Parent []string
}

// Kinds of refusal that the core owns. The adapter's rules bring their own
// codes through provider.CLIRefusal.
const (
	kindToken   = "TOKEN"
	kindNoCLI   = "NOCLI"
	kindLoop    = "LOOP"
	kindContext = "CONTEXT"
)

// Refusal is an error that ends the shim before the CLI starts. Its text
// starts with a code that scripts can match and holds no secret.
type Refusal struct {
	Code    string
	Message string
}

// Error returns the code and the message, without the "agent-id: " prefix
// that the composition root adds.
func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// ExitCode returns the process exit code, ExitRefused.
func (r *Refusal) ExitCode() int { return ExitRefused }

// Errors that the lookup of the real CLI reports to the handler.
var (
	errNoCLI = errors.New("cliint: no real CLI on PATH")
	errLoop  = errors.New("cliint: the CLI on PATH is the launcher")
)

func codeFor(binary, kind string) string {
	name := strings.ToUpper(binary)
	if !binaryPattern.MatchString(binary) {
		name = "CLI"
	}
	return "AID-" + name + "-" + kind
}
