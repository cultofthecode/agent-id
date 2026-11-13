package main

import (
	"context"
	"errors"
	"os"

	"github.com/cultofthecode/agent-id/internal/gitint/credentialhelper"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// newCredentialHelper returns the production handler of git-credential: the
// provider host from the exec-time view of the profile (no key, no request),
// and the Git credential from the token cache, minted with the profile's
// provider.
func newCredentialHelper() *credentialhelper.Handler {
	profiles := lazyProfiles{}
	return &credentialhelper.Handler{
		Host: func(name string) (string, error) {
			p, err := profiles.Resolve(name)
			if err != nil {
				return "", err
			}
			a, err := productionProviders{}.Activator(p)
			if err != nil {
				return "", err
			}
			return a.Host(), nil
		},
		Credential: helperCredential,
	}
}

// helperCredential loads the profile's provider (and with it the key), takes
// a token from the cache, and returns the provider's Git credential. A
// failure carries a fixed class word and never the text of the cause.
func helperCredential(ctx context.Context, name string) (provider.GitCredential, error) {
	p, err := lazyProfiles{}.Resolve(name)
	if err != nil {
		return provider.GitCredential{}, &credentialhelper.ClassedError{Class: "profile", Err: err}
	}
	prov, err := openProvider(p)
	if err != nil {
		return provider.GitCredential{}, &credentialhelper.ClassedError{Class: "private key", Err: err}
	}
	dir, err := token.Dir(os.Getenv)
	if err != nil {
		return provider.GitCredential{}, &credentialhelper.ClassedError{Class: "cache not private", Err: err}
	}
	tok, err := (&token.Manager{Dir: dir}).Token(ctx, prov)
	if err != nil {
		return provider.GitCredential{}, tokenFailure(err)
	}
	return prov.GitCredential(tok), nil
}

// tokenFailure labels a token manager error with its class word. The
// provider's own classes (unauthorized, rate limited, and so on) pass
// through for the handler to name.
func tokenFailure(err error) error {
	classes := []struct {
		target error
		class  string
	}{
		{token.ErrInsecureCache, "cache not private"},
		{token.ErrNoCacheDir, "cache not private"},
		{token.ErrCacheWrite, "cache write failed"},
		{token.ErrLockTimeout, "lock timeout"},
		{token.ErrUnusableToken, "unusable token"},
		{token.ErrInvalidKey, "error"},
	}
	for _, c := range classes {
		if errors.Is(err, c.target) {
			return &credentialhelper.ClassedError{Class: c.class, Err: err}
		}
	}
	return err
}
