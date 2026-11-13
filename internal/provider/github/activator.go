package github

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// decoyToken is the fixed, non-secret value that exec gives gh in place of a
// token. gh takes GH_TOKEN before hosts.yml and the keyring, so it never
// reaches the developer's login; GitHub answers the decoy with 401.
const decoyToken = "agent-id-no-token"

// ErrNotReady is returned by Ready when the profile has no resolved bot
// identity.
var ErrNotReady = errors.New("github: bot identity is not resolved")

// activator implements provider.Activator for one github profile.
type activator struct {
	name     string
	settings ProfileSettings
}

// NewActivator returns the exec-time view of a github profile. It decodes
// the profile's block and reads no key and makes no request.
func NewActivator(p profile.Profile) (provider.Activator, error) {
	settings, err := DecodeProfileSettings(p.Host, p.Settings)
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", profile.Quote(p.Name), err)
	}
	return &activator{name: p.Name, settings: settings}, nil
}

func (a *activator) Name() string { return providerName }

func (a *activator) Host() string { return host }

func (a *activator) GitHosts() []string { return []string{host, "ssh.github.com", "gist.github.com"} }

func (a *activator) CredentialVariables() []string {
	return []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST"}
}

func (a *activator) TokenPrefixes() []string { return slices.Clone(tokenPrefixes) }

func (a *activator) NeutralCLI() provider.NeutralCLI {
	return provider.NeutralCLI{
		ConfigDirVar:         "GH_CONFIG_DIR",
		Decoys:               map[string]string{"GH_TOKEN": decoyToken, "GH_ENTERPRISE_TOKEN": decoyToken},
		Binary:               "gh",
		ForbiddenConfigFiles: []string{"hosts.yml"},
	}
}

// Identity implements provider.Activator from the cached bot identity.
func (a *activator) Identity() (provider.Identity, error) {
	if a.settings.Identity == nil {
		return provider.Identity{}, fmt.Errorf("%w; run agent-id setup github --profile %s", ErrNotReady, a.name)
	}
	return CommitIdentity(a.settings.Identity.Login, a.settings.Identity.UserID)
}

// Ready implements provider.Activator: the identity cache must be present.
func (a *activator) Ready() error {
	if a.settings.Identity == nil {
		return fmt.Errorf("%w; run agent-id setup github --profile %s", ErrNotReady, a.name)
	}
	return nil
}
