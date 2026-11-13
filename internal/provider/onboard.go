package provider

import (
	"context"
	"encoding/json"
)

// Onboarder is one provider's onboarding (ADR #35, setup spec): the setup
// flags it needs and the verification that turns their values into a
// profile block. There is one per provider, not per profile. The setup
// command names no provider; each registered Onboarder adds its own setup
// subcommand.
type Onboarder interface {
	// Name is the setup subcommand and the profile's provider, for example
	// "github".
	Name() string
	// SetupFields returns the provider flags in display order. Every field
	// is required.
	SetupFields() []SetupField
	// Check tests the values without I/O: their syntax, and the refusal of
	// a value that looks like a token or key. A refused value is never
	// echoed.
	Check(r SetupRequest) error
	// Onboard calls Check, then verifies the values with the provider over
	// the network and returns the profile block to store. It writes nothing
	// and its result holds no secret.
	Onboard(ctx context.Context, r SetupRequest) (SetupResult, error)
}

// SetupField is one provider flag of setup: its name without dashes, such
// as "app-id", and a hint that says where to find the value.
type SetupField struct {
	Name  string
	Usage string
}

// SetupRequest is the input of an onboarding.
type SetupRequest struct {
	// Values holds one entry per SetupField, exactly as typed.
	Values map[string]string
	// Dir is the working directory, for a relative path.
	Dir string
}

// SetupResult is a verified onboarding. It holds no secret.
type SetupResult struct {
	// Host is the profile's host, for example "github.com".
	Host string
	// Settings is the provider block to store, identity cache included.
	Settings json.RawMessage
	// Account is the agent's account on the host, for example
	// "claude-dev[bot]".
	Account string
	// Identity is the commit author and committer of the agent.
	Identity Identity
	// Details are provider lines for the summary, in display order.
	Details []SetupDetail
	// Repositories are the repositories the agent can reach, "owner/name",
	// sorted.
	Repositories []string
}

// SetupDetail is one provider line of the setup summary.
type SetupDetail struct {
	Label string
	Value string
}
