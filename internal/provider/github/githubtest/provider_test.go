package githubtest

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestAppProviderMeetsTheContract(t *testing.T) {
	providertest.RunContract(t, func(t *testing.T) providertest.Subject {
		app := NewApp(t, testenv.NewFakeAPI(t))
		app.AddInstallation(installationID, "cultofthecode/widgets")
		return providertest.Subject{
			Provider:         app.Provider(installationID),
			FailTokens:       app.failTokensWith,
			SetTokenLifetime: app.SetTokenLifetime,
		}
	})
}

func TestAppProviderSharesTheFakeAPIState(t *testing.T) {
	app, client := newApp(t)
	app.AddInstallation(installationID, "cultofthecode/widgets")
	token, err := app.Provider(installationID).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokens := app.Tokens(); len(tokens) != 1 || tokens[0].Value != token.Value {
		t.Error("Tokens() does not record the token issued through the provider interface")
	}
	call(t, client, http.MethodGet, app.URL+"/installation/repositories", "token "+token.Value, http.StatusOK, nil)
}

func TestAppProviderGitHubSpecifics(t *testing.T) {
	app, _ := newApp(t)
	p := app.Provider(installationID)
	identity, err := p.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Name != "aidtest-agent[bot]" || identity.Email != "900001+aidtest-agent[bot]@users.noreply.github.com" {
		t.Errorf("Identity() = %+v, want the bot login and its noreply address", identity)
	}
	token := provider.Token{Value: testenv.FakeToken("x")}
	if got := p.GitCredential(token); got.Username != "x-access-token" || got.Host != "github.com" {
		t.Errorf("GitCredential() = %v, want x-access-token on github.com", got)
	}
}

func TestAppProviderStatusErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, provider.ErrUnauthorized},
		{http.StatusForbidden, provider.ErrForbidden},
		{http.StatusNotFound, provider.ErrNotFound},
		{http.StatusInternalServerError, provider.ErrUnavailable},
		{http.StatusBadGateway, provider.ErrUnavailable},
		{http.StatusServiceUnavailable, provider.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			app, _ := newApp(t)
			app.AddInstallation(installationID)
			app.FailTokens(tt.status)
			if _, err := app.Provider(installationID).Token(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("Token() with HTTP %d: error %v, want %v", tt.status, err, tt.want)
			}
		})
	}
	t.Run("unknown installation", func(t *testing.T) {
		app, _ := newApp(t)
		if _, err := app.Provider(installationID).Token(context.Background()); !errors.Is(err, provider.ErrNotFound) {
			t.Errorf("Token() for an unknown installation: error %v, want ErrNotFound", err)
		}
	})
	t.Run("unexpected status", func(t *testing.T) {
		app, _ := newApp(t)
		app.AddInstallation(installationID)
		app.FailTokens(http.StatusTeapot)
		_, err := app.Provider(installationID).Token(context.Background())
		for _, known := range []error{provider.ErrUnauthorized, provider.ErrForbidden, provider.ErrNotFound, provider.ErrUnavailable} {
			if errors.Is(err, known) {
				t.Errorf("Token() with HTTP 418: error %v matches %v, want an unclassified error", err, known)
			}
		}
		if err == nil {
			t.Error("Token() with HTTP 418 succeeded, want an error")
		}
	})
}
