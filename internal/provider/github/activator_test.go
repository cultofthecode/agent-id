package github

import (
	"errors"
	"slices"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

const (
	resolvedBlock   = `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"claude-dev[bot]","user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`
	unresolvedBlock = `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem"}`
)

func newActivator(t *testing.T, block string) provider.Activator {
	t.Helper()
	a, err := NewActivator(profile.Profile{Name: "reviewer", Provider: "github", Host: "github.com", Settings: []byte(block)})
	if err != nil {
		t.Fatalf("NewActivator: %v", err)
	}
	return a
}

func TestActivatorContract(t *testing.T) {
	providertest.RunActivatorContract(t,
		func(t *testing.T) provider.Activator { return newActivator(t, resolvedBlock) },
		func(t *testing.T) provider.Activator { return newActivator(t, unresolvedBlock) })
}

func TestActivatorGitHubValues(t *testing.T) {
	testenv.Isolate(t)
	a := newActivator(t, resolvedBlock)
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		if !slices.Contains(a.CredentialVariables(), name) {
			t.Errorf("CredentialVariables() lacks %s", name)
		}
	}
	if !slices.Contains(a.GitHosts(), "github.com") || !slices.Contains(a.GitHosts(), "ssh.github.com") || !slices.Contains(a.GitHosts(), "gist.github.com") {
		t.Errorf("GitHosts() = %v", a.GitHosts())
	}
	if got := a.NeutralCLI().Decoys["GH_TOKEN"]; got != "agent-id-no-token" {
		t.Errorf("decoy = %q", got)
	}
}

func TestActivatorNotReadyNamesTheProfile(t *testing.T) {
	testenv.Isolate(t)
	err := newActivator(t, unresolvedBlock).Ready()
	if !errors.Is(err, ErrNotReady) || err.Error() != "github: bot identity is not resolved; run agent-id setup github --profile reviewer" {
		t.Errorf("Ready() = %v", err)
	}
}

func TestActivatorRefusesAnInvalidBlock(t *testing.T) {
	testenv.Isolate(t)
	_, err := NewActivator(profile.Profile{Name: "x", Provider: "github", Host: "github.com", Settings: []byte(`{"app_id":1}`)})
	if err == nil {
		t.Fatal("NewActivator accepted an incomplete block")
	}
}

func TestActivatorIdentityFromTheCache(t *testing.T) {
	testenv.Isolate(t)
	a := newActivator(t, resolvedBlock)
	id, err := a.Identity()
	if err != nil || id.Name != "claude-dev[bot]" || id.Email != "3+claude-dev[bot]@users.noreply.github.com" {
		t.Errorf("Identity() = %+v, %v", id, err)
	}
	if _, err := newActivator(t, unresolvedBlock).Identity(); !errors.Is(err, ErrNotReady) {
		t.Errorf("Identity() of an unresolved profile: %v", err)
	}
}
