package profile

import (
	"strconv"
	"strings"
)

// Member names of the core schema.
const (
	memberVersion  = "version"
	memberProfiles = "profiles"
	memberProvider = "provider"
	memberHost     = "host"
)

// maxNameLength is the longest profile or provider name.
const maxNameLength = 32

// maxHostLength is the longest host name.
const maxHostLength = 253

// Parse validates a configuration document and returns it. It is a pure
// function: it touches no file system and no network. Each provider block of
// a provider in registry goes to that provider's validator; a profile of any
// other provider passes the core rules only and stays unsupported until a
// later Resolve. When the document has several problems, Parse returns the
// first in this order: syntax and size, inline secrets, duplicate members,
// version, the top level, then each profile in document order.
func Parse(data []byte, registry *Registry) (Config, error) {
	root, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	if err := scanSecrets(root, registry.tokenPrefixes()); err != nil {
		return Config{}, err
	}
	if err := checkDuplicates(root, ""); err != nil {
		return Config{}, err
	}
	profiles, err := checkTopLevel(root)
	if err != nil {
		return Config{}, err
	}
	config := Config{Version: Version, Profiles: make(map[string]Profile, len(profiles.members))}
	for _, m := range profiles.members {
		p, err := checkProfile(m.name, m.value, registry)
		if err != nil {
			return Config{}, err
		}
		config.Profiles[p.Name] = p
	}
	return config, nil
}

// checkDuplicates returns ErrConfigInvalid for the first object, in document
// order, that holds a member name twice.
func checkDuplicates(n *node, path string) error {
	switch n.kind {
	case kindObject:
		seen := make(map[string]bool, len(n.members))
		for _, m := range n.members {
			if seen[m.name] {
				return Errorf(ErrConfigInvalid, "%s: duplicate member %s", orTopLevel(path), Quote(m.name))
			}
			seen[m.name] = true
		}
		for _, m := range n.members {
			if err := checkDuplicates(m.value, child(path, m.name)); err != nil {
				return err
			}
		}
	case kindArray:
		for i, e := range n.elems {
			if err := checkDuplicates(e, path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkTopLevel checks the document object, its version, and its members,
// and returns the profiles object.
func checkTopLevel(root *node) (*node, error) {
	if root.kind != kindObject {
		return nil, Errorf(ErrConfigInvalid, "the configuration must be a JSON object")
	}
	if err := checkVersion(root.get(memberVersion)); err != nil {
		return nil, err
	}
	profiles := root.get(memberProfiles)
	switch {
	case profiles == nil:
		return nil, Errorf(ErrConfigInvalid, "profiles is missing")
	case profiles.kind != kindObject:
		return nil, Errorf(ErrConfigInvalid, "profiles must be an object")
	}
	for _, m := range root.members {
		if m.name != memberVersion && m.name != memberProfiles {
			return nil, Errorf(ErrConfigInvalid, "unknown member %s", Quote(m.name))
		}
	}
	return profiles, nil
}

// checkVersion accepts exactly the JSON integer 1.
func checkVersion(version *node) error {
	switch {
	case version == nil:
		return Errorf(ErrUnsupportedVersion, "version is missing")
	case version.kind == kindNumber && version.text == strconv.Itoa(Version):
		return nil
	case version.kind == kindNumber && isPositiveInteger(version.text):
		return Errorf(ErrUnsupportedVersion, "version %s is newer than this agent-id supports (%d); upgrade agent-id", sanitize(version.text), Version)
	default:
		return Errorf(ErrUnsupportedVersion, "version must be the integer %d", Version)
	}
}

// isPositiveInteger reports whether a JSON number literal is a positive
// integer without sign, fraction, or exponent.
func isPositiveInteger(literal string) bool {
	return literal != "" && literal[0] != '0' && strings.Trim(literal, "0123456789") == ""
}

// checkProfile checks the profile name and value of one member of profiles
// and returns the profile.
func checkProfile(name string, value *node, registry *Registry) (Profile, error) {
	if !validProfileName(name) {
		return Profile{}, Errorf(ErrConfigInvalid, `profiles: invalid profile name %s: use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"`, Quote(name))
	}
	path := memberProfiles + "." + name
	if value.kind != kindObject {
		return Profile{}, Errorf(ErrConfigInvalid, "%s must be an object", path)
	}
	provider, err := checkProvider(path, value.get(memberProvider))
	if err != nil {
		return Profile{}, err
	}
	host, err := checkHost(path, value.get(memberHost))
	if err != nil {
		return Profile{}, err
	}
	block, err := checkBlock(path, provider, value)
	if err != nil {
		return Profile{}, err
	}
	settings := raw(block)
	if reg, ok := registry.lookup(provider); ok {
		if err := reg.Validator.ValidateSettings(host, settings); err != nil {
			return Profile{}, providerError(path, err)
		}
	}
	return Profile{Name: name, Provider: provider, Host: host, Settings: settings}, nil
}

// checkProvider checks the provider member of the profile at path.
func checkProvider(path string, provider *node) (string, error) {
	switch {
	case provider == nil:
		return "", Errorf(ErrConfigInvalid, "%s.provider is missing", path)
	case provider.kind != kindString || !validName(provider.text):
		return "", Errorf(ErrConfigInvalid, "%s.provider: invalid provider name", path)
	}
	return provider.text, nil
}

// checkHost checks the syntax of the host member of the profile at path.
// Which hosts a provider accepts is the provider's rule.
func checkHost(path string, host *node) (string, error) {
	switch {
	case host == nil:
		return "", Errorf(ErrConfigInvalid, "%s.host is missing", path)
	case host.kind != kindString:
		return "", Errorf(ErrUnsupportedHost, "%s.host: must be a string", path)
	case !validHost(host.text):
		return "", Errorf(ErrUnsupportedHost, "%s.host: %s is not a valid host name (lowercase, no scheme, port or path)", path, Quote(host.text))
	}
	return host.text, nil
}

// checkBlock checks that the profile at path has an object named after its
// provider and no member besides provider, host, and that block.
func checkBlock(path, provider string, profile *node) (*node, error) {
	block := profile.get(provider)
	switch {
	case block == nil:
		return nil, Errorf(ErrConfigInvalid, "%s: provider block %s is missing", path, Quote(provider))
	case block.kind != kindObject:
		return nil, Errorf(ErrConfigInvalid, "%s.%s must be an object", path, provider)
	}
	for _, m := range profile.members {
		if m.name != memberProvider && m.name != memberHost && m.name != provider {
			return nil, Errorf(ErrConfigInvalid, "%s: unknown member %s", path, Quote(m.name))
		}
	}
	return block, nil
}

// providerError prefixes the error of a provider's validator with the path
// of the profile. Its only class is the block class the error wraps, or
// ErrConfigInvalid; the original error is not wrapped, so that an adapter
// can neither make a broken profile pass nor claim a class such as
// ErrConfigNotFound that changes what callers do.
func providerError(path string, err error) error {
	return Errorf(blockSentinelOf(err), "%s.%s", path, err.Error())
}

// validProfileName reports whether s is a valid profile name: a valid name
// that does not end with "-" (#44).
func validProfileName(s string) bool {
	return validName(s) && !strings.HasSuffix(s, "-")
}

// validName reports whether s is a valid provider name, and the base rule of
// a profile name: a lowercase letter, then up to 31 lowercase letters,
// digits, or "-".
func validName(s string) bool {
	if s == "" || len(s) > maxNameLength || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	return strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}

// validHost reports whether s is a lowercase DNS host name of at least two
// labels, with no scheme, port, path, trailing dot, or whitespace.
func validHost(s string) bool {
	if len(s) > maxHostLength {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

// validLabel reports whether s is one label of a lowercase host name.
func validLabel(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	return strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}

// orTopLevel names the top level of the document in a message.
func orTopLevel(path string) string {
	if path == "" {
		return "(top level)"
	}
	return path
}
