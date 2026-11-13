package githubtest

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Host is the host the fake App stands in for.
const Host = "github.com"

// statusErrors maps the HTTP status of a failed token request to the
// provider error that the GitHub adapter reports for it.
var statusErrors = map[int]error{
	http.StatusUnauthorized:    provider.ErrUnauthorized,
	http.StatusForbidden:       provider.ErrForbidden,
	http.StatusNotFound:        provider.ErrNotFound,
	http.StatusTooManyRequests: provider.ErrRateLimited,
}

// errorStatuses maps a provider error to the status that FailTokens sets for
// it.
var errorStatuses = map[error]int{
	provider.ErrUnauthorized: http.StatusUnauthorized,
	provider.ErrForbidden:    http.StatusForbidden,
	provider.ErrNotFound:     http.StatusNotFound,
	provider.ErrUnavailable:  http.StatusServiceUnavailable,
	provider.ErrRateLimited:  http.StatusTooManyRequests,
}

// Provider returns the fake App, bound to installation id, as a
// provider.Provider: a test adapter that shares the App's state, so a token
// it issues is valid on the fake HTTP API and listed by Tokens. Core tests
// use providertest.Fake instead; this adapter lets GitHub-side tests run the
// provider contract against the fake GitHub App.
func (a *App) Provider(installationID int64) provider.Provider {
	return appProvider{app: a, installationID: installationID}
}

// appProvider is the fake App behind the provider interface.
type appProvider struct {
	app            *App
	installationID int64
}

// Name implements provider.Provider.
func (p appProvider) Name() string { return "github" }

// Host implements provider.Provider.
func (p appProvider) Host() string { return Host }

// TokenSubject implements provider.Provider as the GitHub adapter does.
func (p appProvider) TokenSubject() string {
	return "app-" + strconv.FormatInt(AppID, 10) + "-installation-" + strconv.FormatInt(p.installationID, 10)
}

// Token implements provider.Provider: it mints an installation token as
// POST /app/installations/{id}/access_tokens does.
func (p appProvider) Token(ctx context.Context) (provider.Token, error) {
	if err := ctx.Err(); err != nil {
		return provider.Token{}, fmt.Errorf("github: create an installation token: %w", err)
	}
	token, status := p.app.mint(p.installationID, time.Now())
	if status != http.StatusCreated {
		return provider.Token{}, fmt.Errorf("github: create an installation token: HTTP %d: %w", status, statusError(status))
	}
	return provider.Token{Value: token.Value, ExpiresAt: token.ExpiresAt}, nil
}

// Identity implements provider.Provider with the App's bot user.
func (p appProvider) Identity(ctx context.Context) (provider.Identity, error) {
	if err := ctx.Err(); err != nil {
		return provider.Identity{}, fmt.Errorf("github: resolve the bot user: %w", err)
	}
	return provider.Identity{Name: BotLogin(), Email: BotEmail()}, nil
}

// GitCredential implements provider.Provider the way GitHub expects an
// installation token over HTTPS.
func (p appProvider) GitCredential(token provider.Token) provider.GitCredential {
	return provider.GitCredential{Protocol: "https", Host: Host, Username: "x-access-token", Password: token.Value}
}

// CLIEnvironment implements provider.Provider for gh.
func (p appProvider) CLIEnvironment(token provider.Token) []string {
	return []string{"GH_TOKEN=" + token.Value, "GH_HOST=" + Host}
}

// BotEmail returns the noreply address of the App's bot user,
// "<id>+<slug>[bot]@users.noreply.github.com".
func BotEmail() string {
	return strconv.FormatInt(BotUserID, 10) + "+" + BotLogin() + "@users.noreply.github.com"
}

// statusError returns the provider error for a failed token request.
func statusError(status int) error {
	if err, ok := statusErrors[status]; ok {
		return err
	}
	if status >= http.StatusInternalServerError {
		return provider.ErrUnavailable
	}
	return fmt.Errorf("unexpected status %d", status)
}

// FailTokensWith makes token requests fail with the HTTP status that GitHub
// answers for err, one of the provider errors, or succeed again when err is
// nil. It is the FailTokens control of providertest.Subject.
func (a *App) FailTokensWith(err error) {
	a.FailTokens(errorStatuses[err])
}
