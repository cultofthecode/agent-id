// Package setup is the setup slice: it onboards an agent profile through a
// provider's onboarder and writes the verified profile (setup spec,
// .ai/specs/2026-11-13-setup-github.md). It names no forge: every provider
// brings its own subcommand, flags, and verification through
// provider.Onboarder. Nothing is written before the onboarder has verified
// the values, and no secret is printed.
package setup

import (
	"errors"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Input is one setup request: the profile name, the provider, and the values
// of the provider's setup fields as typed.
type Input struct {
	Profile  string
	Provider string
	Values   map[string]string
	Dir      string
}

// Output is a written profile, for the summary.
type Output struct {
	Profile      string
	Provider     string
	Host         string
	Account      string
	Identity     provider.Identity
	Details      []provider.SetupDetail
	Repositories []string
	Created      bool
	ConfigPath   string
}

// Errors of setup. Every error of the handler wraps one of them, a provider
// error, a profile store error, or the onboarder's own class.
var (
	ErrProfileRequired  = errors.New("setup: --profile is required")
	ErrMissingFlags     = errors.New("setup: required flags are missing")
	ErrUnknownProvider  = errors.New("setup: unknown provider")
	ErrProviderConflict = errors.New("setup: the profile belongs to another provider")
)
