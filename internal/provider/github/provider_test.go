package github

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestAdapterMeetsTheContract(t *testing.T) {
	providertest.RunContract(t, func(t *testing.T) providertest.Subject {
		app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
		app.AddInstallation(installationID, "cultofthecode/widgets")
		return providertest.Subject{
			Provider:         newAdapter(t, app),
			FailTokens:       app.FailTokensWith,
			SetTokenLifetime: app.SetTokenLifetime,
		}
	})
}

func TestGitAndCLIIntegration(t *testing.T) {
	app := newFakeApp(t)
	p := newAdapter(t, app)
	token := provider.Token{Value: testenv.FakeToken("git")}
	if got := p.GitCredential(token); got != (provider.GitCredential{Protocol: "https", Host: "github.com", Username: "x-access-token", Password: token.Value}) {
		t.Errorf("GitCredential() = %v, want x-access-token on github.com", got)
	}
	env := p.CLIEnvironment(token)
	if len(env) != 2 || env[0] != "GH_TOKEN="+token.Value || env[1] != "GH_HOST=github.com" {
		t.Error("CLIEnvironment() does not give gh the token in GH_TOKEN with GH_HOST=github.com")
	}
	if p.Name() != "github" || p.Host() != "github.com" {
		t.Errorf("Name(), Host() = %q, %q", p.Name(), p.Host())
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		status int
		header http.Header
		want   error
	}{
		{http.StatusUnauthorized, nil, provider.ErrUnauthorized},
		{http.StatusForbidden, nil, provider.ErrForbidden},
		{http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"12"}}, provider.ErrForbidden},
		{http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}}, provider.ErrRateLimited},
		{http.StatusForbidden, http.Header{"Retry-After": {"60"}}, provider.ErrRateLimited},
		{http.StatusNotFound, nil, provider.ErrNotFound},
		{http.StatusTooManyRequests, nil, provider.ErrRateLimited},
		{http.StatusUnprocessableEntity, nil, provider.ErrUnavailable},
		{http.StatusInternalServerError, nil, provider.ErrUnavailable},
		{http.StatusServiceUnavailable, nil, provider.ErrUnavailable},
		{http.StatusTemporaryRedirect, nil, provider.ErrUnavailable},
		{http.StatusOK, nil, provider.ErrUnavailable},
	}
	for _, tt := range tests {
		got := classify(tt.status, tt.header)
		if got != tt.want {
			t.Errorf("classify(%d, %v) = %v, want %v", tt.status, tt.header, got, tt.want)
		}
		if tt.want != provider.ErrRateLimited && errors.Is(got, provider.ErrRateLimited) {
			t.Errorf("classify(%d, %v) is rate limiting", tt.status, tt.header)
		}
	}
}
