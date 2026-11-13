package activation

import (
	"net/url"
	"strings"
)

// specificity ranks how closely a http.<url>.* pattern matches a URL, as
// Git's urlmatch does: host first, then path.
type specificity struct{ host, path int }

func (a specificity) less(b specificity) bool {
	if a.host != b.host {
		return a.host < b.host
	}
	return a.path < b.path
}

// EffectiveHeaders returns the names of the extra HTTP headers that Git
// would send for rawURL, from the ordered entries of git config --list. An
// entry whose pattern is less specific than the best one seen is ignored, as
// in Git, and an empty value resets the list. It is conservative: a generic
// http.extraHeader always applies.
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
		if spec != nil {
			if seen && spec.less(best) {
				continue
			}
			best, seen = *spec, true
		}
		if e.Value == "" {
			headers = nil
			continue
		}
		name, _, _ := strings.Cut(e.Value, ":")
		headers = append(headers, clean(strings.TrimSpace(name), 40))
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
	if !strings.EqualFold(pattern.Hostname(), target.Hostname()) || port(pattern) != port(target) {
		return nil, false
	}
	pp := strings.TrimSuffix(pattern.Path, "/")
	tp := target.Path
	if pp != "" && tp != pp && !strings.HasPrefix(tp, pp+"/") {
		return nil, false
	}
	return &specificity{host: len(pattern.Host), path: len(pp)}, true
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
