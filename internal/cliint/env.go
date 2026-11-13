package cliint

import (
	"slices"
	"strings"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// BuildEnv returns the environment of the real CLI. It starts from parent,
// removes the provider's credential variables and the configuration
// directory variable, sets the provider's decoys again (they keep the other
// host closed), pins the configuration directory to configDir, adds the
// provider's variables for the token (cliEnv), and sets the recursion marker.
// An entry of parent without a name or without "=" is dropped. The result has
// each name once. cliEnv entries must be NAME=value with a plain upper-case
// name outside the AGENT_ID_ prefix and a value without a newline or NUL;
// otherwise BuildEnv returns false.
func BuildEnv(parent []string, cli provider.NeutralCLI, credentialVariables, cliEnv []string, configDir string) ([]string, bool) {
	set := map[string]string{}
	for name, value := range cli.Decoys {
		set[name] = value
	}
	for _, kv := range cliEnv {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !envNamePat.MatchString(name) || strings.HasPrefix(name, agentIDPre) || strings.ContainsAny(value, "\n\x00") || value == "" {
			return nil, false
		}
		set[name] = value
	}
	if cli.ConfigDirVar != "" {
		set[cli.ConfigDirVar] = configDir
	}
	set[VarActive] = "1"
	out := make([]string, 0, len(parent)+len(set))
	for _, kv := range parent {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || slices.Contains(credentialVariables, name) {
			continue
		}
		if _, replaced := set[name]; replaced {
			continue
		}
		out = append(out, kv)
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		out = append(out, name+"="+set[name])
	}
	return out, true
}
