package activation

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var profileName = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// NormalizeEnv returns the parent environment as one value per name, sorted
// by name (#72). An entry without "=" or with an empty name is discarded,
// and the last occurrence of a name wins. Names compare byte for byte. Every
// reader of the parent environment uses this one view, so a check and the
// child never see different values.
func NormalizeEnv(env []string) []string {
	return render(parseEnv(env))
}

// parseEnv returns the variables of env as NormalizeEnv sees them.
func parseEnv(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if ok && name != "" {
			out[name] = value
		}
	}
	return out
}

// ReadContext returns the agent context that parent carries, or nil when it
// carries none. Stray AGENT_ID_* variables without AGENT_ID_CONTEXT are not a
// context.
func ReadContext(parent []string) (*Context, error) {
	env := parseEnv(parent)
	version, ok := env[VarContext]
	if !ok {
		return nil, nil
	}
	if version != ContextVersion {
		return nil, invalid("from an incompatible agent-id (%s is not %s)", VarContext, ContextVersion)
	}
	name := env[VarProfile]
	if name == "" {
		return nil, invalid("damaged: %s is missing", VarProfile)
	}
	if !profileName.MatchString(name) {
		return nil, invalid("damaged: %s is not a valid profile name", VarProfile)
	}
	base, err := atoiStrict(env[VarGitBase])
	if err != nil {
		return nil, invalid("damaged: %s is missing or not a number", VarGitBase)
	}
	end, err := atoiStrict(env[VarGitEnd])
	if err != nil {
		return nil, invalid("damaged: %s is missing or not a number", VarGitEnd)
	}
	if base > end {
		return nil, invalid("damaged: %s is greater than %s", VarGitBase, VarGitEnd)
	}
	dirs, names, err := readContribution(env)
	if err != nil {
		return nil, err
	}
	return &Context{Version: version, Profile: name, Mechanism: env[VarMechanism], Binary: env[VarBinary], GitConfigBase: base, GitConfigEnd: end, PathPrepend: dirs, SetNames: names}, nil
}

// readContribution reads the recorded PATH directories and Set names. A
// missing variable is damage; the empty string is the empty list.
func readContribution(env map[string]string) (dirs, names []string, err error) {
	rawDirs, ok := env[VarPathPrepend]
	if !ok {
		return nil, nil, invalid("damaged: %s is missing", VarPathPrepend)
	}
	rawNames, ok := env[VarSetNames]
	if !ok {
		return nil, nil, invalid("damaged: %s is missing", VarSetNames)
	}
	if rawDirs != "" {
		dirs = strings.Split(rawDirs, ":")
	}
	if rawNames != "" {
		names = strings.Split(rawNames, ",")
	}
	for _, dir := range dirs {
		if !strings.HasPrefix(dir, "/") {
			return nil, nil, invalid("damaged: %s has a directory that is not absolute", VarPathPrepend)
		}
	}
	if hasDuplicate(dirs) {
		return nil, nil, invalid("damaged: %s has a duplicate directory", VarPathPrepend)
	}
	for _, name := range names {
		if !validSetName(name) {
			return nil, nil, invalid("damaged: %s has a name that is not valid", VarSetNames)
		}
	}
	if hasDuplicate(names) || !slices.IsSorted(names) {
		return nil, nil, invalid("damaged: %s is not a sorted list without duplicates", VarSetNames)
	}
	return dirs, names, nil
}

func hasDuplicate(list []string) bool {
	seen := make(map[string]bool, len(list))
	for _, e := range list {
		if seen[e] {
			return true
		}
		seen[e] = true
	}
	return false
}

// Decide relates a start of profile to the inherited context: a fresh start
// without one, a re-entry for the same profile, and ErrNestedProfile for any
// other.
func Decide(ctx *Context, profile string) (Start, error) {
	if ctx == nil {
		return StartFresh, nil
	}
	if ctx.Profile != profile {
		return StartFresh, failf(ErrNestedProfile, "already in an agent context for profile %q; refusing to start profile %q inside it (nested exec with a different profile is not supported); start it from a terminal outside the agent context", ctx.Profile, profile)
	}
	return StartReenter, nil
}

func invalid(format string, args ...any) error {
	return failf(ErrContextInvalid, "the inherited agent context is "+format+"; start from a terminal outside the agent context", args...)
}

// clean cuts s to n characters and replaces control characters with '?'.
func clean(s string, n int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == n {
			break
		}
		if r < 0x20 || r == 0x7f {
			r = '?'
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

// atoiStrict parses a non-negative decimal integer without sign or spaces.
func atoiStrict(s string) (int, error) {
	if s == "" || len(s) > 9 {
		return 0, strconv.ErrSyntax
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, strconv.ErrSyntax
		}
	}
	return strconv.Atoi(s)
}
