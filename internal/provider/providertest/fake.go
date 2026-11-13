// Package providertest holds the contract every provider.Provider meets and a
// provider-neutral fake for core tests. Core tests use Fake; an adapter's
// tests run RunContract against the adapter's own fake host. Only test files
// import this package.
package providertest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Identity of the fake provider: test data on a reserved domain, never a
// real host or account.
const (
	FakeName     = "fake"
	FakeHost     = "git.agent-id.test"
	FakeUsername = "agent-id-fake"
	FakeCLIVar   = "FAKE_CLI_TOKEN"
	FakeSubject  = "fake-installation"
	defaultTTL   = time.Hour
	// FakeBot is the bot login that VerifyIdentity reports, and
	// FakeRepository the repository that Repositories lists, until a test
	// sets others.
	FakeBot        = "fake-bot[bot]"
	FakeRepository = "fake-owner/fake-repo"
)

// Fake is an in-memory provider.Provider that knows no real host. It issues
// testenv.FakeToken tokens, and its token lifetime and failures are
// controllable.
type Fake struct {
	mu       sync.Mutex
	lifetime time.Duration
	subject  string
	failure  error
	tokens   []provider.Token

	repositories    []string
	repositoriesErr error
	identity        provider.IdentityCheck
	identityErr     error
}

// NewFake returns a Fake whose tokens live one hour.
func NewFake() *Fake {
	return &Fake{
		lifetime:     defaultTTL,
		subject:      FakeSubject,
		repositories: []string{FakeRepository},
		identity:     provider.IdentityCheck{Bot: FakeBot, Cached: FakeBot, Match: true},
	}
}

// Name implements provider.Provider.
func (f *Fake) Name() string { return FakeName }

// Host implements provider.Provider.
func (f *Fake) Host() string { return FakeHost }

// TokenSubject implements provider.Provider. It is FakeSubject unless
// SetTokenSubject changed it.
func (f *Fake) TokenSubject() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subject
}

// SetTokenSubject sets the subject that TokenSubject reports, so that a test
// can stand in for two installations with two fakes.
func (f *Fake) SetTokenSubject(subject string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subject = subject
}

// Token implements provider.Provider.
func (f *Fake) Token(ctx context.Context) (provider.Token, error) {
	if err := ctx.Err(); err != nil {
		return provider.Token{}, fmt.Errorf("fake provider: issue a token: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure != nil {
		return provider.Token{}, fmt.Errorf("fake provider: issue a token: %w", f.failure)
	}
	token := provider.Token{
		Value:     testenv.FakeToken(fmt.Sprintf("fake_%d", len(f.tokens)+1)),
		ExpiresAt: time.Now().Add(f.lifetime).Truncate(time.Second),
	}
	f.tokens = append(f.tokens, token)
	return token, nil
}

// Identity implements provider.Provider.
func (f *Fake) Identity(ctx context.Context) (provider.Identity, error) {
	if err := ctx.Err(); err != nil {
		return provider.Identity{}, fmt.Errorf("fake provider: resolve the identity: %w", err)
	}
	return provider.Identity{Name: "agent-id fake bot", Email: "bot@" + FakeHost}, nil
}

// GitCredential implements provider.Provider.
func (f *Fake) GitCredential(token provider.Token) provider.GitCredential {
	return provider.GitCredential{Protocol: "https", Host: FakeHost, Username: FakeUsername, Password: token.Value}
}

// CLIEnvironment implements provider.Provider.
func (f *Fake) CLIEnvironment(token provider.Token) []string {
	return []string{FakeCLIVar + "=" + token.Value}
}

// Describe implements provider.Provider.
func (f *Fake) Describe() []provider.Field {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []provider.Field{
		{Label: "Identity", Value: f.identity.Cached},
		{Label: "Installation", Value: f.subject},
		{Label: "Credentials", Value: "fake installation token"},
	}
}

// VerifyIdentity implements provider.Provider with the answer that
// SetIdentityCheck or FailIdentity set.
func (f *Fake) VerifyIdentity(ctx context.Context) (provider.IdentityCheck, error) {
	if err := ctx.Err(); err != nil {
		return provider.IdentityCheck{}, fmt.Errorf("fake provider: verify the identity: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.identityErr != nil {
		return provider.IdentityCheck{}, fmt.Errorf("fake provider: verify the identity: %w", f.identityErr)
	}
	return f.identity, nil
}

// Repositories implements provider.Provider: it lists the repositories set
// with SetRepositories for a token that the fake issued.
func (f *Fake) Repositories(ctx context.Context, token provider.Token) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fake provider: list the repositories: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.repositoriesErr != nil {
		return nil, fmt.Errorf("fake provider: list the repositories: %w", f.repositoriesErr)
	}
	if !slices.ContainsFunc(f.tokens, func(t provider.Token) bool { return t.Value == token.Value }) {
		return nil, fmt.Errorf("fake provider: list the repositories: %w", provider.ErrUnauthorized)
	}
	return slices.Clone(f.repositories), nil
}

// SetRepositories sets the "owner/name" list that Repositories returns.
func (f *Fake) SetRepositories(repositories ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repositories = slices.Clone(repositories)
}

// FailRepositories makes Repositories fail with err until it is called with
// nil.
func (f *Fake) FailRepositories(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repositoriesErr = err
}

// SetIdentityCheck sets the answer of VerifyIdentity and the cached identity
// that Describe reports.
func (f *Fake) SetIdentityCheck(check provider.IdentityCheck) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identity = check
}

// FailIdentity makes VerifyIdentity fail with err until it is called with
// nil.
func (f *Fake) FailIdentity(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identityErr = err
}

// FailTokens makes Token fail with err, one of the provider errors, until it
// is called with nil.
func (f *Fake) FailTokens(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failure = err
}

// SetTokenLifetime sets the lifetime of the tokens issued from now on. A
// lifetime of zero or less issues tokens that have already expired.
func (f *Fake) SetTokenLifetime(lifetime time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lifetime = lifetime
}

// Tokens returns the tokens the fake issued, in order.
func (f *Fake) Tokens() []provider.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokens)
}

// Subject returns the fake with its controls, for RunContract.
func (f *Fake) Subject() Subject {
	return Subject{Provider: f, FailTokens: f.FailTokens, SetTokenLifetime: f.SetTokenLifetime}
}
