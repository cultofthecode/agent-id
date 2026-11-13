package activation

import (
	"net/url"
	"regexp"
	"strings"
)

// specificity ranks how closely a http.<url>.* pattern matches a URL, as
// Git's urlmatch does: host first (a wildcard host ranks below an exact one),
// then path, then user.
type specificity struct{ host, user, path int }

func (a specificity) less(b specificity) bool {
	if a.host != b.host {
		return a.host < b.host
	}
	if a.path != b.path {
		return a.path < b.path
	}
	return a.user < b.user
}

// EffectiveHeaders returns the names of the extra HTTP headers that Git
// would send for rawURL, from the ordered entries of git config --list. An
// entry whose pattern is less specific than the best one seen is ignored, as
// in Git, and an empty value resets the list. A pattern with a user matches
// only URLs with that user and ranks above one without. It is conservative:
// a generic http.extraHeader always applies, and a non-empty header of a less
// specific pattern after a more specific one is still counted (Git ignores
// it), which can only refuse too much.
func EffectiveHeaders(entries []ConfigEntry, rawURL string) []string {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	var headers []string
	var best specificity
	seen := false
	for _, e := range entries {
		spec, ok := headerScope(e.Key, target)
		if !ok {
			continue
		}
		if spec == nil {
			spec = &specificity{}
		}
		if seen && spec.less(best) && e.Value == "" {
			continue
		}
		if !spec.less(best) || !seen {
			best, seen = *spec, true
		}
		if e.Value == "" {
			headers = nil
			continue
		}
		headers = append(headers, headerName(e.Value))
	}
	return headers
}

// headerScope reports whether key is an http extraheader key that applies to
// target. The specificity is nil for the generic http.extraheader.
func headerScope(key string, target *url.URL) (*specificity, bool) {
	section, sub, variable, ok := splitKey(key)
	if !ok || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") {
		return nil, false
	}
	if sub == "" {
		return nil, true
	}
	pattern, err := url.Parse(sub)
	if err != nil || pattern.Host == "" || !strings.EqualFold(pattern.Scheme, target.Scheme) {
		return nil, false
	}
	exact := strings.EqualFold(pattern.Hostname(), target.Hostname())
	if (!exact && !wildcardMatches(pattern.Hostname(), target.Hostname())) || port(pattern) != port(target) {
		return nil, false
	}
	user := 0
	if pattern.User != nil {
		if target.User == nil || pattern.User.String() != target.User.String() {
			return nil, false
		}
		user = len(pattern.User.String())
	}
	pp := strings.TrimSuffix(pattern.Path, "/")
	tp := target.Path
	if pp != "" && tp != pp && !strings.HasPrefix(tp, pp+"/") {
		return nil, false
	}
	hostRank := 1
	if exact {
		hostRank = len(pattern.Hostname()) + 1
	}
	return &specificity{host: hostRank, user: user, path: len(pp)}, true
}

func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}

// headerName returns the name of an extra header for a message, never its
// value: the text before the colon when it looks like a header name, and a
// fixed word otherwise.
func headerName(value string) string {
	name, _, ok := strings.Cut(value, ":")
	name = strings.TrimSpace(name)
	if !ok || !headerNamePattern.MatchString(name) {
		return "unnamed header"
	}
	return name
}

var headerNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)

// ScopedHeaders returns the names of extra headers that a path-scoped
// http.<url>.extraheader entry on one of hosts would send for URLs under its
// path. agent-id's reset covers the host, and Git ignores a less specific
// reset for a more specific entry, so these survive.
func ScopedHeaders(entries []ConfigEntry, hosts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range entries {
		section, sub, variable, ok := splitKey(e.Key)
		if !ok || sub == "" || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") || seen[sub] {
			continue
		}
		pattern, err := url.Parse(sub)
		if err != nil || !hostIn(pattern.Hostname(), hosts) {
			continue
		}
		seen[sub] = true
		out = append(out, EffectiveHeaders(entries, sub)...)
	}
	return out
}

// wildcardMatches reports whether pattern is a "*.suffix" host pattern that
// matches host.
func wildcardMatches(pattern, host string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(pattern), "*")
	return ok && strings.HasPrefix(rest, ".") && strings.HasSuffix(strings.ToLower(host), rest)
}

func hostIn(host string, hosts []string) bool {
	for _, h := range hosts {
		if strings.EqualFold(strings.TrimSuffix(host, "."), h) || wildcardMatches(host, h) {
			return true
		}
	}
	return false
}
