package provider

// Activator is what exec needs to know about a profile's provider without a
// secret and without the network: the hosts and variables to neutralize and
// whether the profile can be activated. An adapter builds one from the
// profile alone; it never reads the private key (exec spec, #20, #68).
type Activator interface {
	// Name identifies the provider, for example "github".
	Name() string
	// Host is the host name whose Git and API traffic the provider serves.
	Host() string
	// GitHosts are the host names whose Git remotes belong to the provider,
	// and that Host is one of.
	GitHosts() []string
	// CredentialVariables are the CLI variables that the core removes from
	// every child. The list is never empty.
	CredentialVariables() []string
	// TokenPrefixes are the prefixes of the provider's tokens, for the
	// secret guard of the child environment.
	TokenPrefixes() []string
	// NeutralCLI describes how the provider's CLI is neutralized.
	NeutralCLI() NeutralCLI
	// Ready reports, without a network call, whether the profile can be
	// activated. Its error carries the adapter's hint.
	Ready() error
	// Identity returns the bot's commit author and committer from the
	// profile's cache, with no network call and no key read. It fails when
	// the cache is missing or malformed.
	Identity() (Identity, error)
	// CheckCLI decides, as a pure function of the CLI's arguments and the
	// parent environment, whether the shim may start the CLI for the agent
	// (#23). It returns nil to allow and a *CLIRefusal to refuse. It reads
	// no key and makes no request.
	CheckCLI(args, parent []string) error
}

// CLIRefusal is a refusal of an invocation of the provider's CLI. Its text
// holds no secret: no token, header, key path, or value of the parent's
// variables.
type CLIRefusal struct {
	// Code is the stable identifier that scripts can match, for example
	// AID-GH-AUTH.
	Code string
	// Message says what is refused, why, and what the agent can do instead.
	Message string
}

// Error returns the code and the message.
func (r *CLIRefusal) Error() string { return r.Code + ": " + r.Message }

// NeutralCLI is how a provider's CLI is closed in an agent context: it gets
// an empty private configuration directory and a fixed, non-secret decoy
// instead of a token, so it can never use the developer's login.
type NeutralCLI struct {
	// ConfigDirVar names the variable that moves the CLI's configuration
	// directory, for example GH_CONFIG_DIR.
	ConfigDirVar string
	// Decoys maps variable names, each one of CredentialVariables, to a fixed
	// value that is no secret and has no token prefix.
	Decoys map[string]string
	// Binary is the CLI's command name, for example gh.
	Binary string
	// ForbiddenConfigFiles are file names that must not exist in the CLI
	// configuration directory, for example hosts.yml.
	ForbiddenConfigFiles []string
}
