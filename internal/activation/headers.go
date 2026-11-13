package activation

import (
	"net/url"
	"regexp"
	"strings"
)

// specificity ranks how closely an http.<url>.* pattern matches a URL, in the
// order of Git's urlmatch: the length of the host pattern first, so a
// pattern with a wildcard label ranks below the exact host. The path length
// comes next and the user length last.
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
	if !hostPatternMatches(pattern.Hostname(), target.Hostname()) || port(pattern) != port(target) {
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
	return &specificity{host: len(pattern.Hostname()), user: user, path: len(pp)}, true
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

// HostHeader is an extra HTTP header, by name, that Git sends to Host.
type HostHeader struct{ Host, Name string }

// ScopedHeaders returns the extra headers that a scoped http.<url>.extraheader
// entry would send to one of hosts. Scoped means a path, a user, or a host
// pattern. It evaluates each entry at every host in hosts that the entry's
// host pattern matches, so a pattern that Git ranks below agent-id's reset
// for that host, such as *.com, reports nothing. agent-id's reset covers the
// host only, and Git ignores a less specific reset for a more specific
// entry, so a header of a path- or user-scoped entry survives.
func ScopedHeaders(entries []ConfigEntry, hosts []string) []HostHeader {
	var out []HostHeader
	seen := map[string]bool{}
	for _, e := range entries {
		section, sub, variable, ok := splitKey(e.Key)
		if !ok || sub == "" || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") || seen[sub] {
			continue
		}
		pattern, err := url.Parse(sub)
		if err != nil || pattern.Host == "" {
			continue
		}
		seen[sub] = true
		for _, h := range hosts {
			if !hostPatternMatches(pattern.Hostname(), h) {
				continue
			}
			for _, name := range EffectiveHeaders(entries, atHost(pattern, h)) {
				out = append(out, HostHeader{Host: h, Name: name})
			}
		}
	}
	return out
}

// atHost returns the URL of pattern with its host replaced by host.
func atHost(pattern *url.URL, host string) string {
	at := *pattern
	at.Host = host
	if p := pattern.Port(); p != "" {
		at.Host = host + ":" + p
	}
	return at.String()
}

// hostPatternMatches reports whether pattern, a host pattern of
// http.<url>.*, matches host as Git does: label by label, with the same
// number of labels, where "*" matches exactly one label.
func hostPatternMatches(pattern, host string) bool {
	pl := strings.Split(strings.ToLower(strings.TrimSuffix(pattern, ".")), ".")
	hl := strings.Split(strings.ToLower(strings.TrimSuffix(host, ".")), ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if pl[i] != "*" && pl[i] != hl[i] {
			return false
		}
	}
	return true
}
