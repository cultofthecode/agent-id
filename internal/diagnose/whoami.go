package diagnose

import (
	"fmt"
	"time"
)

// Values of WhoamiReport.Context outside and inside an agent context.
const (
	ContextPersonal = "personal/unmanaged"
	ContextAgent    = "agent"
)

// Classes of a failed token, the closed list of the JSON contract.
const (
	ClassUnauthorized = "unauthorized"
	ClassForbidden    = "forbidden"
	ClassNotFound     = "not found"
	ClassRateLimited  = "rate limited"
	ClassUnavailable  = "unavailable"
	ClassKey          = "key"
	ClassConfig       = "config"
	ClassCache        = "cache"
)

// Token statuses of the JSON contract.
const (
	TokenValid       = "valid"
	TokenUnavailable = "unavailable"
	TokenNotChecked  = "not checked"
)

// WhoamiToken is the token status of whoami. It holds the expiry, a class
// and a reason, never a token.
type WhoamiToken struct {
	Status    string
	ExpiresAt time.Time
	// ExpiresIn is the time left; the text rounds it down to minutes.
	ExpiresIn time.Duration
	Class     string
	Reason    string
}

// WhoamiReport is the result of agent-id whoami. Outside an agent context
// only Context is set. An empty string means "not determined".
type WhoamiReport struct {
	Context string
	// Problem is why the context is damaged or incompatible; empty
	// otherwise.
	Problem string
	Profile string
	// ProfileNote follows the profile in the text, for example that the
	// configuration has no such profile. The JSON ignores it.
	ProfileNote string
	Provider    string
	Host        string
	Identity    string
	Details     []Detail
	Token       WhoamiToken
	Activation  string
	// Warnings are problems that do not change the exit status, for
	// example a shell function named like the provider's CLI.
	Warnings []string
}

// Detail is one provider-specific line of whoami.
type Detail struct {
	Label string
	Value string
}

// ExitCode is 0 outside a context and inside one with a valid token, and 1
// otherwise.
func (r WhoamiReport) ExitCode() int {
	if r.Context == ContextPersonal || r.Problem == "" && r.Token.Status == TokenValid {
		return ExitOK
	}
	return ExitFailure
}

// contextLine is the value of the Context label.
func (r WhoamiReport) contextLine() string {
	if r.Problem == "" {
		return r.Context
	}
	return r.Context + " (" + r.Problem + ")"
}

// tokenLine is the value of the Token status label.
func (r WhoamiReport) tokenLine() string {
	switch r.Token.Status {
	case TokenValid:
		return fmt.Sprintf("valid (expires in %dm)", int(r.Token.ExpiresIn/time.Minute))
	case TokenUnavailable:
		return "unavailable (" + r.Token.Class + ": " + r.Token.Reason + ")"
	}
	return "not checked (" + r.Token.Reason + ")"
}
