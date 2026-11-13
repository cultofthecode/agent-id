// Package whoami is the whoami slice: it prints which context, profile, bot,
// installation, credential and token the current shell uses, without a
// secret (spec .ai/specs/2026-11-13-whoami-doctor-output.md). Outside an
// agent context it prints one line and reads no configuration.
package whoami

import (
	"context"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// Input is one whoami request.
type Input struct {
	JSON bool
}

// Context is what the context variables of agent-id exec say. Present is
// false outside an agent context. Problem is not empty for a damaged or an
// incompatible context, for example "damaged: AGENT_ID_ACTIVE_PROFILE is
// missing" or "incompatible context version 2". Profile and Mechanism may be
// empty then.
type Context struct {
	Present   bool
	Profile   string
	Mechanism string
	Problem   string
}

// ProfileResolver is the part of the profile store that whoami uses.
type ProfileResolver interface {
	Resolve(name string) (profile.Profile, error)
}

// TokenSource is the part of the token manager that whoami uses.
type TokenSource interface {
	Token(ctx context.Context, src token.Source) (provider.Token, error)
}
