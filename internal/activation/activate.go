package activation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cultofthecode/agent-id/internal/gitint"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// ProfileResolver is the port to the profile store.
type ProfileResolver interface {
	CheckName(name string) error
	Resolve(name string) (profile.Profile, error)
}

// ProviderSource builds the exec-time view of a resolved profile's provider.
// It reads no key and makes no request.
type ProviderSource interface {
	Activator(p profile.Profile) (provider.Activator, error)
}

// Deps are the ports of Activate. Checks are built by the composition root
// from the provider and a Runner, since the provider depends on the profile.
type Deps struct {
	Profiles  ProfileResolver
	Providers ProviderSource
	Neutral   NeutralDirs
	// Checks builds the remote checks for the profile's provider.
	Checks func(a provider.Activator) []Check
}

// Request is one activation.
type Request struct {
	Profile   string
	Mechanism string
	Binary    string
	Dir       string
	Parent    []string
}

// Activation is the child environment and what was removed from it.
type Activation struct {
	Env  []string
	Drop []string
}

// coreDrop are the variables of every provider that a prompt or helper could
// use to answer for the developer.
var coreDrop = []string{"GIT_ASKPASS", "SSH_ASKPASS"}

// Activate runs the activation pipeline in its fixed order: the profile name,
// the inherited context and nesting, the profile and its readiness, the
// inherited Git configuration, the neutral directories, the child
// environment, and the remote checks with that environment. It opens no
// network connection and writes only the neutral directories.
func Activate(ctx context.Context, d Deps, r Request) (Activation, error) {
	if err := d.Profiles.CheckName(r.Profile); err != nil {
		return Activation{}, err
	}
	inherited, err := ReadContext(r.Parent)
	if err != nil {
		return Activation{}, err
	}
	if _, err := Decide(inherited, r.Profile); err != nil {
		return Activation{}, err
	}
	p, err := d.Profiles.Resolve(r.Profile)
	if err != nil {
		return Activation{}, err
	}
	a, err := d.Providers.Activator(p)
	if err != nil {
		return Activation{}, err
	}
	if err := a.Ready(); err != nil {
		return Activation{}, &detailed{kind: ErrProfileNotReady, msg: fmt.Sprintf("profile %q: %s", r.Profile, err), cause: err}
	}
	git, err := gitContribution(r.Profile, a)
	if err != nil {
		return Activation{}, err
	}
	if len(a.CredentialVariables()) == 0 {
		return Activation{}, unsafePlan("no credential variables to remove (the provider declares none)")
	}
	if err := CheckInheritedGit(r.Parent); err != nil {
		return Activation{}, err
	}
	paths, err := d.Neutral.Ensure(r.Parent, a.Name(), a.NeutralCLI())
	if err != nil {
		return Activation{}, err
	}
	plan := PlanFor(r, a, paths)
	plan = withGit(plan, git)
	env, err := Build(r.Parent, plan)
	if err != nil {
		return Activation{}, err
	}
	for _, check := range d.Checks(a) {
		if err := check(ctx, r.Dir, env); err != nil {
			return Activation{}, err
		}
	}
	return Activation{Env: env, Drop: plan.Drop}, nil
}

// PlanFor returns the plan of #20: the context, the drop list, and the
// neutral credential baseline (#68).
func PlanFor(r Request, a provider.Activator, paths NeutralPaths) Plan {
	cli := a.NeutralCLI()
	set := map[string]string{"GIT_TERMINAL_PROMPT": "0"}
	if cli.ConfigDirVar != "" {
		set[cli.ConfigDirVar] = paths.ConfigDir
	}
	for name, value := range cli.Decoys {
		set[name] = value
	}
	drop := slices.Concat(coreDrop, a.CredentialVariables())
	return Plan{
		Profile:   r.Profile,
		Mechanism: r.Mechanism,
		Binary:    r.Binary,
		Drop:      drop,
		Set:       set,
		GitConfig: []GitConfigEntry{
			{Key: "credential.helper", Value: ""},
			{Key: "http.https://" + a.Host() + "/.extraHeader", Value: ""},
			{Key: "core.askPass", Value: ""},
		},
		PathPrepend:   []string{paths.BinDir},
		TokenPrefixes: a.TokenPrefixes(),
	}
}

func gitContribution(profileName string, a provider.Activator) (gitint.Contribution, error) {
	id, err := a.Identity()
	if err != nil {
		return gitint.Contribution{}, &detailed{kind: ErrIdentityUnavailable, cause: err,
			msg: fmt.Sprintf("profile %q: cannot read the bot identity: %s", profileName, err)}
	}
	c, err := gitint.Identity(id)
	if err != nil {
		return gitint.Contribution{}, &detailed{kind: ErrIdentityUnavailable, cause: err,
			msg: fmt.Sprintf("profile %q: the cached bot identity is malformed (%s); run agent-id doctor --profile %s", profileName, ruleOf(err), profileName)}
	}
	return c, nil
}

func ruleOf(err error) string {
	msg := strings.TrimPrefix(err.Error(), gitint.ErrIdentityInvalid.Error())
	return strings.Trim(msg, " ()")
}

func withGit(plan Plan, c gitint.Contribution) Plan {
	for name, value := range c.Set {
		plan.Set[name] = value
	}
	for _, e := range c.GitConfig {
		plan.GitConfig = append(plan.GitConfig, GitConfigEntry{Key: e.Key, Value: e.Value})
	}
	return plan
}
