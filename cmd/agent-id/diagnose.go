package main

import (
	"context"
	"os"
	"regexp"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/diagnose/doctor"
	"github.com/cultofthecode/agent-id/internal/diagnose/whoami"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// diagnosticCommands returns whoami and doctor wired to the production
// profile store, token manager, and environment, with the provider
// registration and the provider opener that the composition root supplies.
func diagnosticCommands(registry profile.Registration, openProvider func(profile.Profile) (provider.Provider, error)) []*cobra.Command {
	home, _ := os.UserHomeDir()
	output := diagnose.Output{Patterns: diagnose.NewPatterns(registry.TokenPrefixes), Home: home}
	who := &whoami.Handler{
		ReadContext: func() whoami.Context { return readContext(os.LookupEnv) },
		Profiles: func() (whoami.ProfileResolver, error) {
			store, err := openProfileStore()
			if err != nil {
				return nil, err
			}
			return store, nil
		},
		OpenProvider: openProvider,
		Tokens:       tokenManager{},
		Output:       output,
	}
	doc := &doctor.Handler{
		OpenStore: func() (doctor.Store, error) {
			store, err := openProfileStore()
			if err != nil {
				return nil, err
			}
			return store, nil
		},
		Supports:     func(name string) bool { return name == registry.Name },
		OpenProvider: openProvider,
		Git:          doctor.ExecGit{Env: os.Environ()},
		CacheDir:     func() (string, error) { return token.Dir(os.Getenv) },
		Environ:      os.Environ,
		Output:       output,
	}
	return []*cobra.Command{whoami.NewCommand(who), doctor.NewCommand(doc)}
}

// tokenManager hands out cached tokens from the token cache of the current
// user, the same cache that the credential helper will use.
type tokenManager struct{}

// Token implements whoami.TokenSource.
func (tokenManager) Token(ctx context.Context, src token.Source) (provider.Token, error) {
	dir, err := token.Dir(os.Getenv)
	if err != nil {
		return provider.Token{}, err
	}
	return (&token.Manager{Dir: dir}).Token(ctx, src)
}

// profileNamePattern is the shape of a profile name that exec puts in
// AGENT_ID_ACTIVE_PROFILE (exec spec, context variables).
var profileNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// contextVersion is the version of the context contract that this agent-id
// understands.
const contextVersion = "1"

// readContext reads the context variables of agent-id exec (exec spec,
// "The context variables"). It is the extension point for the exec slice:
// when exec merges, whoami and doctor switch to activation.ReadContext, so
// that both commands and the refusals of exec share one reader. Until then
// this function applies the same rules to the variables that whoami reports.
func readContext(lookup func(string) (string, bool)) whoami.Context {
	version, present := lookup("AGENT_ID_CONTEXT")
	if !present {
		return whoami.Context{}
	}
	c := whoami.Context{Present: true}
	c.Profile, _ = lookup("AGENT_ID_ACTIVE_PROFILE")
	c.Mechanism, _ = lookup("AGENT_ID_MECHANISM")
	if version != contextVersion {
		c.Problem = "incompatible context version " + version
		return c
	}
	c.Problem = contextProblem(c, lookup)
	return c
}

// contextProblem returns why a version-1 context is damaged, or "".
func contextProblem(c whoami.Context, lookup func(string) (string, bool)) string {
	switch {
	case !profileNamePattern.MatchString(c.Profile):
		return "damaged: AGENT_ID_ACTIVE_PROFILE is missing or not a profile name"
	case c.Mechanism != "exec" && c.Mechanism != "env":
		return "damaged: AGENT_ID_MECHANISM is missing or unknown"
	}
	base, okBase := lookupInt(lookup, "AGENT_ID_GIT_CONFIG_BASE")
	end, okEnd := lookupInt(lookup, "AGENT_ID_GIT_CONFIG_END")
	if !okBase || !okEnd || base > end {
		return "damaged: AGENT_ID_GIT_CONFIG_BASE or AGENT_ID_GIT_CONFIG_END is missing or not valid"
	}
	count := 0
	if v, ok := lookup("GIT_CONFIG_COUNT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return "damaged: GIT_CONFIG_COUNT is not an integer"
		}
		count = n
	}
	if count != end {
		return "damaged: the Git entries changed after activation"
	}
	return ""
}

// lookupInt returns the non-negative integer in the variable name.
func lookupInt(lookup func(string) (string, bool), name string) (int, bool) {
	v, ok := lookup(name)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n >= 0
}
