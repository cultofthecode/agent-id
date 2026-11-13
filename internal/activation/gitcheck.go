package activation

import (
	"fmt"
	"strconv"
	"strings"
)

// maxGitConfigCount is the largest GIT_CONFIG_COUNT that agent-id accepts.
const maxGitConfigCount = 1000

// forbiddenSections are the sections whose keys an inherited
// GIT_CONFIG_PARAMETERS must not set, in lower case. An include can pull in
// any of the others.
var forbiddenSections = map[string]bool{"credential": true, "url": true, "http": true, "gpg": true, "include": true, "includeif": true}

// forbiddenKeys are the signing keys that are refused as a whole.
var forbiddenKeys = map[string]bool{
	"commit.gpgsign": true, "tag.gpgsign": true, "tag.forcesignannotated": true,
	"push.gpgsign": true, "user.signingkey": true,
}

// inheritedCount returns the inherited GIT_CONFIG_COUNT: unset or empty
// counts as 0.
func inheritedCount(env map[string]string) (int, error) {
	v, ok := env["GIT_CONFIG_COUNT"]
	if !ok || v == "" {
		return 0, nil
	}
	n, err := atoiStrict(v)
	if err != nil || n > maxGitConfigCount {
		return 0, failf(ErrInheritedGitConfig, "inherited GIT_CONFIG_COUNT is not a number from 0 to %d; every git command would fail; fix or unset the GIT_CONFIG_* variables", maxGitConfigCount)
	}
	return n, nil
}

// CheckInheritedGit refuses a malformed GIT_CONFIG_COUNT set and a
// GIT_CONFIG_PARAMETERS that sets a credential, URL, HTTP, include, or
// signing key. It is pure and never echoes a value.
func CheckInheritedGit(parent []string) error {
	env := parseEnv(parent)
	n, err := inheritedCount(env)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		for _, prefix := range []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"} {
			if _, ok := env[prefix+strconv.Itoa(i)]; !ok {
				return failf(ErrInheritedGitConfig, "inherited GIT_CONFIG_COUNT=%d has no %s%d; every git command would fail; fix or unset the GIT_CONFIG_* variables", n, prefix, i)
			}
		}
	}
	params, ok := env["GIT_CONFIG_PARAMETERS"]
	if !ok {
		return nil
	}
	keys, err := ParseGitConfigParameters(params)
	if err != nil {
		return failf(ErrInheritedGitConfig, "inherited GIT_CONFIG_PARAMETERS cannot be parsed; unset it or fix it")
	}
	for _, key := range keys {
		if name, bad := forbiddenKey(key); bad {
			return failf(ErrInheritedGitConfig, "inherited GIT_CONFIG_PARAMETERS sets %s, which would override agent-id's credentials; unset GIT_CONFIG_PARAMETERS or remove that setting", name)
		}
	}
	return nil
}

// forbiddenKey reports whether key is refused and returns its section and
// variable name without the subsection, which can hold a credential.
func forbiddenKey(key string) (string, bool) {
	lower := strings.ToLower(key)
	section, rest, hasDot := strings.Cut(lower, ".")
	if !hasDot {
		return "", false
	}
	if !forbiddenSections[section] && !forbiddenKeys[lower] {
		return "", false
	}
	variable := rest
	if i := strings.LastIndex(rest, "."); i >= 0 {
		variable = rest[i+1:]
		return clean(section, 32) + ".<subsection>." + clean(variable, 32), true
	}
	return clean(section+"."+variable, 64), true
}

// ParseGitConfigParameters returns the keys that a GIT_CONFIG_PARAMETERS
// value sets, with Git's quoting: 'key'='value', 'key=value', and a bare
// 'key', separated by spaces, with the '\” and '\!' escapes.
func ParseGitConfigParameters(v string) ([]string, error) {
	var keys []string
	i := 0
	for {
		for i < len(v) && v[i] == ' ' {
			i++
		}
		if i == len(v) {
			return keys, nil
		}
		first, next, err := dequote(v, i)
		if err != nil {
			return nil, err
		}
		i = next
		key := first
		if i < len(v) && v[i] == '=' {
			_, next, err := dequote(v, i+1)
			if err != nil {
				return nil, err
			}
			i = next
		} else if k, _, ok := strings.Cut(first, "="); ok {
			key = k
		}
		if i < len(v) && v[i] != ' ' {
			return nil, fmt.Errorf("unexpected character in GIT_CONFIG_PARAMETERS")
		}
		keys = append(keys, key)
	}
}

// dequote reads one single-quoted string of v starting at i and returns it
// and the index after it.
func dequote(v string, i int) (string, int, error) {
	if i >= len(v) || v[i] != '\'' {
		return "", 0, fmt.Errorf("expected a quote")
	}
	i++
	var b strings.Builder
	for i < len(v) {
		if v[i] != '\'' {
			b.WriteByte(v[i])
			i++
			continue
		}
		i++
		if i+2 < len(v) && v[i] == '\\' && (v[i+1] == '\'' || v[i+1] == '!') && v[i+2] == '\'' {
			b.WriteByte(v[i+1])
			i += 3
			continue
		}
		return b.String(), i, nil
	}
	return "", 0, fmt.Errorf("unterminated quote")
}
