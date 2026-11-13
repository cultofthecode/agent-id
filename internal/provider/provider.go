// Package provider is the boundary between agent-id's core and a code host
// (ADR #35). GitHub is the first adapter (internal/provider/github); GitLab
// and Bitbucket are later implementations of the same interface. Core
// packages depend on this package only, never on a host-specific package.
//
// The interface is provisional: it holds what the test harness exercises,
// one method per responsibility the ADR names (tokens, identity, the
// per-host Git credential, CLI integration, host mapping). The GitHub adapter
// (#15) settles it.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Classes of failure that every provider reports, so that core can fail
// closed without knowing the host. Match them with errors.Is.
var (
	ErrUnauthorized = errors.New("provider: the host rejected the agent's credentials")
	ErrForbidden    = errors.New("provider: the host denied access")
	ErrNotFound     = errors.New("provider: the host does not know the agent's installation or account")
	ErrUnavailable  = errors.New("provider: the host is unavailable")
)

// Provider is one code host that gives an agent its own identity and
// short-lived credentials.
type Provider interface {
	// Name identifies the provider in a profile, for example "github".
	Name() string
	// Host is the host name whose Git and API traffic the provider serves,
	// for example "github.com".
	Host() string
	// Token returns a new access token for the agent.
	Token(ctx context.Context) (Token, error)
	// Identity returns the commit author and committer of the agent.
	Identity(ctx context.Context) (Identity, error)
	// GitCredential returns the answer of a Git credential helper for Host
	// over HTTPS with token.
	GitCredential(token Token) GitCredential
	// CLIEnvironment returns the variables, as NAME=value, that give the
	// host's CLI (gh, later glab) token. The result holds a secret.
	CLIEnvironment(token Token) []string
}

// Token is a short-lived access token. Formatting it with any verb of the
// fmt package prints its expiry, never its value.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Format implements fmt.Formatter and redacts the value.
func (t Token) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "provider.Token{Value: [redacted], ExpiresAt: "+t.ExpiresAt.UTC().Format(time.RFC3339)+"}")
}

// Identity is the commit author and committer of an agent.
type Identity struct {
	Name  string
	Email string
}

// GitCredential is the answer of a Git credential helper. Formatting it with
// any verb of the fmt package never prints the password.
type GitCredential struct {
	Protocol string
	Host     string
	Username string
	Password string
}

// Format implements fmt.Formatter and redacts the password.
func (c GitCredential) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "provider.GitCredential{Protocol: "+c.Protocol+", Host: "+c.Host+", Username: "+c.Username+", Password: [redacted]}")
}
