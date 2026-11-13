package providertest

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Identity of the fake activator: test data, never a real host.
const (
	FakeCredentialVar = "FAKE_CLI_TOKEN"
	FakeDecoy         = "fake-no-token"
	FakeCLIBinary     = "fakecli"
	FakeConfigDirVar  = "FAKE_CLI_CONFIG_DIR"
)

// FakeActivator is a provider.Activator that knows no real host. Its fields
// are public so that a test can make it hostile (an empty drop list, a
// failing Ready).
type FakeActivator struct {
	Hosts      []string
	Variables  []string
	Prefixes   []string
	CLI        provider.NeutralCLI
	ReadyError error
	// Ident is the identity returned by Identity; IdentityError, when set,
	// makes Identity fail.
	Ident         provider.Identity
	IdentityError error
}

// NewFakeActivator returns a ready activator for the fake host.
func NewFakeActivator() *FakeActivator {
	return &FakeActivator{
		Hosts:     []string{FakeHost, "ssh." + FakeHost},
		Variables: []string{FakeCredentialVar, FakeCredentialVar + "_ALIAS"},
		Prefixes:  []string{testenv.FakeTokenPrefix},
		Ident:     provider.Identity{Name: "agent-id fake bot", Email: "1+fake-bot@" + FakeHost},
		CLI: provider.NeutralCLI{
			ConfigDirVar:         FakeConfigDirVar,
			Decoys:               map[string]string{FakeCredentialVar: FakeDecoy},
			Binary:               FakeCLIBinary,
			ForbiddenConfigFiles: []string{"hosts.yml"},
		},
	}
}

func (f *FakeActivator) Name() string                    { return FakeName }
func (f *FakeActivator) Host() string                    { return FakeHost }
func (f *FakeActivator) GitHosts() []string              { return f.Hosts }
func (f *FakeActivator) CredentialVariables() []string   { return f.Variables }
func (f *FakeActivator) TokenPrefixes() []string         { return f.Prefixes }
func (f *FakeActivator) NeutralCLI() provider.NeutralCLI { return f.CLI }
func (f *FakeActivator) Ready() error                    { return f.ReadyError }

// Identity implements provider.Activator.
func (f *FakeActivator) Identity() (provider.Identity, error) { return f.Ident, f.IdentityError }

var activatorHostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

// RunActivatorContract runs the contract of provider.Activator as subtests.
// newActivator returns a ready activator and, for the not-ready case, one
// whose Ready fails with a hint.
func RunActivatorContract(t *testing.T, ready func(t *testing.T) provider.Activator, notReady func(t *testing.T) provider.Activator) {
	t.Helper()
	t.Run("names hosts and variables", func(t *testing.T) {
		testenv.Isolate(t)
		a := ready(t)
		if a.Name() == "" || !activatorHostPattern.MatchString(a.Host()) {
			t.Errorf("Name() = %q, Host() = %q", a.Name(), a.Host())
		}
		if !containsFold(a.GitHosts(), a.Host()) {
			t.Errorf("GitHosts() = %v, want it to contain Host() %q", a.GitHosts(), a.Host())
		}
		for _, h := range a.GitHosts() {
			if !activatorHostPattern.MatchString(h) {
				t.Errorf("GitHosts() has %q, want bare host names", h)
			}
		}
		if len(a.CredentialVariables()) == 0 {
			t.Error("CredentialVariables() is empty, want the CLI's token variables")
		}
	})
	t.Run("neutral CLI is complete and secret-free", func(t *testing.T) {
		testenv.Isolate(t)
		a := ready(t)
		cli := a.NeutralCLI()
		if cli.Binary == "" || cli.ConfigDirVar == "" || strings.ContainsAny(cli.Binary, "/ ") {
			t.Errorf("NeutralCLI() = %+v, want a binary name and a configuration variable", cli)
		}
		if len(cli.Decoys) == 0 {
			t.Error("NeutralCLI() has no decoy, want a decoy that closes the CLI's keychain fallback")
		}
		for name, value := range cli.Decoys {
			if !contains(a.CredentialVariables(), name) {
				t.Errorf("decoy %s is not one of CredentialVariables()", name)
			}
			if value == "" || profile.LooksSecret(value, a.TokenPrefixes()) {
				t.Errorf("decoy %s has a value that is empty or looks like a secret", name)
			}
		}
		if len(a.TokenPrefixes()) == 0 {
			t.Error("TokenPrefixes() is empty, want the provider's token prefixes")
		}
	})
	t.Run("identity comes from the profile without a key", func(t *testing.T) {
		testenv.Isolate(t)
		id, err := ready(t).Identity()
		if err != nil || id.Name == "" || id.Email == "" {
			t.Errorf("Identity() of a resolved profile = %+v, %v", id, err)
		}
		if _, err := notReady(t).Identity(); err == nil {
			t.Error("Identity() of an unresolved profile succeeded")
		}
	})
	t.Run("ready is silent and not-ready has a hint", func(t *testing.T) {
		testenv.Isolate(t)
		if err := ready(t).Ready(); err != nil {
			t.Errorf("Ready() of a resolved profile: %v", err)
		}
		err := notReady(t).Ready()
		if err == nil || errors.Unwrap(err) == nil && !strings.Contains(err.Error(), "setup") {
			t.Errorf("Ready() of an unresolved profile = %v, want an error with a setup hint", err)
		}
	})
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func containsFold(list []string, s string) bool {
	for _, e := range list {
		if strings.EqualFold(e, s) {
			return true
		}
	}
	return false
}

// RequireDropsCLIEnvironment fails the test when a variable that p's
// CLIEnvironment returns is not one of a.CredentialVariables(). A provider
// cannot then add a variable to CLIEnvironment and forget to drop it from
// the child (exec spec, #75).
func RequireDropsCLIEnvironment(t *testing.T, a provider.Activator, p provider.Provider) {
	t.Helper()
	for _, entry := range p.CLIEnvironment(provider.Token{Value: testenv.FakeToken("drops")}) {
		name, _, _ := strings.Cut(entry, "=")
		if !contains(a.CredentialVariables(), name) {
			t.Errorf("CLIEnvironment() sets %s, which is not one of CredentialVariables()", name)
		}
	}
}
