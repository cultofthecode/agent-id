package providertest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Subject is a provider under contract test, with the controls that the
// contract needs.
type Subject struct {
	Provider provider.Provider
	// FailTokens makes Token fail with err, one of the provider errors; nil
	// restores success.
	FailTokens func(err error)
	// SetTokenLifetime sets the lifetime of the tokens issued from now on.
	SetTokenLifetime func(lifetime time.Duration)
}

// failures are the provider errors a provider must report through errors.Is.
var failures = []error{provider.ErrUnauthorized, provider.ErrForbidden, provider.ErrNotFound, provider.ErrUnavailable, provider.ErrRateLimited}

var (
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	envVarPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`)
)

// RunContract runs the contract of provider.Provider as subtests. Each subtest
// isolates itself with testenv.Isolate and then calls newSubject for a fresh
// provider, so the contract never reaches the network or a real credential.
func RunContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, s Subject)
	}{
		{"names its provider and host", checkNames},
		{"issues an unexpired fake token", checkToken},
		{"reports the token lifetime", checkLifetime},
		{"maps failures to provider errors and fails closed", checkFailures},
		{"stops when the context is canceled", checkCanceled},
		{"resolves a commit identity", checkIdentity},
		{"answers the Git credential helper for its host", checkGitCredential},
		{"gives the CLI the token", checkCLIEnvironment},
		{"never prints a token", checkRedaction},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testenv.Isolate(t)
			c.run(t, newSubject(t))
		})
	}
}

func checkNames(t *testing.T, s Subject) {
	if name := s.Provider.Name(); !namePattern.MatchString(name) {
		t.Errorf("Name() = %q, want a lowercase identifier", name)
	}
	if host := s.Provider.Host(); host == "" || strings.ContainsAny(host, "/@: ") {
		t.Errorf("Host() = %q, want a bare host name", host)
	}
}

func checkToken(t *testing.T, s Subject) {
	token := mustToken(t, s)
	if !testenv.IsFakeToken(token.Value) {
		t.Errorf("Token() returned a token without the %s prefix", testenv.FakeTokenPrefix)
	}
	if !token.ExpiresAt.After(time.Now()) {
		t.Errorf("Token() expires at %v, want a time in the future", token.ExpiresAt)
	}
	if second := mustToken(t, s); second.Value == token.Value {
		t.Error("two calls of Token() returned the same token, want a new token per call")
	}
}

func checkLifetime(t *testing.T, s Subject) {
	for _, lifetime := range []time.Duration{10 * time.Minute, -time.Minute} {
		s.SetTokenLifetime(lifetime)
		before := time.Now().Truncate(time.Second)
		token := mustToken(t, s)
		if got := token.ExpiresAt.Sub(before); got < lifetime-2*time.Second || got > lifetime+2*time.Second {
			t.Errorf("lifetime %v: token expires %v after the request, want the expiry the host reported", lifetime, got)
		}
	}
}

func checkFailures(t *testing.T, s Subject) {
	for _, failure := range failures {
		s.FailTokens(failure)
		token, err := s.Provider.Token(context.Background())
		if !errors.Is(err, failure) {
			t.Errorf("Token() with %q: error %v, want errors.Is %q", failure, err, failure)
		}
		for _, other := range failures {
			if other != failure && !errors.Is(failure, other) && errors.Is(err, other) {
				t.Errorf("Token() with %q: error %v also matches %q, want one class of failure", failure, err, other)
			}
		}
		if token != (provider.Token{}) {
			t.Errorf("Token() with %q returned a token, want the zero Token", failure)
		}
	}
	s.FailTokens(nil)
	mustToken(t, s)
}

func checkCanceled(t *testing.T, s Subject) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Provider.Token(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Token() with a canceled context: error %v, want context.Canceled", err)
	}
	if _, err := s.Provider.Identity(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Identity() with a canceled context: error %v, want context.Canceled", err)
	}
}

func checkIdentity(t *testing.T, s Subject) {
	identity, err := s.Provider.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity(): %v", err)
	}
	if identity.Name == "" || !strings.Contains(identity.Email, "@") {
		t.Errorf("Identity() = %+v, want a name and an email address", identity)
	}
}

func checkGitCredential(t *testing.T, s Subject) {
	token := mustToken(t, s)
	credential := s.Provider.GitCredential(token)
	if credential.Protocol != "https" || credential.Host != s.Provider.Host() || credential.Username == "" {
		t.Errorf("GitCredential() = %v, want https, host %q, and a username", credential, s.Provider.Host())
	}
	if credential.Password != token.Value {
		t.Error("GitCredential() password is not the token")
	}
}

func checkCLIEnvironment(t *testing.T, s Subject) {
	token := mustToken(t, s)
	holders := 0
	for _, entry := range s.Provider.CLIEnvironment(token) {
		if !envVarPattern.MatchString(entry) {
			t.Errorf("CLIEnvironment() has an entry that is not NAME=value: %q", strings.SplitN(entry, "=", 2)[0])
		}
		if strings.HasSuffix(entry, "="+token.Value) {
			holders++
		}
	}
	if holders != 1 {
		t.Errorf("CLIEnvironment() passes the token in %d variables, want exactly one", holders)
	}
}

func checkRedaction(t *testing.T, s Subject) {
	token := mustToken(t, s)
	credential := s.Provider.GitCredential(token)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(verb, token), token.Value) || strings.Contains(fmt.Sprintf(verb, credential), token.Value) {
			t.Errorf("formatting with %s prints the token", verb)
		}
	}
	s.FailTokens(provider.ErrUnauthorized)
	if _, err := s.Provider.Token(context.Background()); err != nil && strings.Contains(err.Error(), token.Value) {
		t.Error("the error of a failed Token() contains an earlier token")
	}
}

// mustToken returns a token of s and fails the test when Token fails.
func mustToken(t *testing.T, s Subject) provider.Token {
	t.Helper()
	token, err := s.Provider.Token(context.Background())
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	return token
}
