package activation

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// specificity ranks how closely an http.<url>.* pattern matches a URL, in the
// order of Git's urlmatch: the length of the host pattern first, so a
// pattern with a wildcard label ranks below the exact host. The path length
// comes next. A pattern with a user part (even an empty one) ranks above one
// without; Git compares only whether the user matched.
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
	target, ok := parseGitURL(rawURL, false)
	if !ok {
		return nil
	}
	return effectiveHeaders(entries, target)
}

func effectiveHeaders(entries []ConfigEntry, target gitURL) []string {
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
// target. The specificity is nil for the generic http.extraheader. A pattern
// that does not parse never applies; UnparsableHeaderScope reports it separately.
func headerScope(key string, target gitURL) (*specificity, bool) {
	section, sub, variable, ok := splitKey(key)
	if !ok || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") {
		return nil, false
	}
	if sub == "" {
		return nil, true
	}
	pattern, ok := parseGitURL(sub, true)
	if !ok || pattern.scheme != target.scheme {
		return nil, false
	}
	if !hostPatternMatches(pattern.host, target.host) || pattern.port != target.port {
		return nil, false
	}
	user := 0
	if pattern.hasUser {
		if !target.hasUser || pattern.user != target.user {
			return nil, false
		}
		user = 1
	}
	pp := strings.TrimSuffix(pattern.path, "/")
	tp := target.path
	if pp != "" && tp != pp && !strings.HasPrefix(tp, pp+"/") {
		return nil, false
	}
	return &specificity{host: len(pattern.host), user: user, path: len(pp)}, true
}

// UnparsableHeaderScope returns the first http.<url>.extraheader entry that
// has a header and a URL pattern that agent-id cannot normalize as Git does.
// Git may still apply such a pattern, so the caller must refuse it. An entry
// with an empty value only resets headers; skipping it never weakens the
// check, because a reset counts only when its pattern parses.
func UnparsableHeaderScope(entries []ConfigEntry) (ConfigEntry, bool) {
	for _, e := range entries {
		section, sub, variable, ok := splitKey(e.Key)
		if !ok || sub == "" || e.Value == "" || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") {
			continue
		}
		if _, ok := parseGitURL(sub, true); !ok {
			return e, true
		}
	}
	return ConfigEntry{}, false
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
//
// Each entry is evaluated at its own URL and at a URL below its path. A reset
// with a longer path, such as /org// against /org/, matches the entry's own
// URL but not the URLs below it, and Git still sends the header there. The
// deep URL ends in a segment longer than any pattern, so only a pattern whose
// path is the entry's path or one of its ancestors can match it.
func ScopedHeaders(entries []ConfigEntry, hosts []string) []HostHeader {
	var out []HostHeader
	seen := map[string]bool{}
	depth := deepSegmentLen(entries)
	for _, e := range entries {
		section, sub, variable, ok := splitKey(e.Key)
		if !ok || sub == "" || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") || seen[sub] {
			continue
		}
		pattern, ok := parseGitURL(sub, true)
		if !ok {
			continue
		}
		seen[sub] = true
		for _, h := range hosts {
			if !hostPatternMatches(pattern.host, h) {
				continue
			}
			at := pattern.atHost(h)
			for _, target := range []gitURL{at, at.below(depth)} {
				for _, name := range effectiveHeaders(entries, target) {
					if hh := (HostHeader{Host: h, Name: name}); !slices.Contains(out, hh) {
						out = append(out, hh)
					}
				}
			}
		}
	}
	return out
}

// HostRootHeaders returns the names of the extra headers that Git sends to
// host over either scheme and on any port. For each scheme it evaluates the
// root and a URL below the root, on the default port and on a port that no
// pattern of entries names. A reset whose path is "//" matches the root but
// not the URLs below it, so both URLs count. A reset for the default port
// does not cover another port, so a generic header always reaches the
// unnamed port, as in Git.
func HostRootHeaders(entries []ConfigEntry, host string) []string {
	var names []string
	depth := deepSegmentLen(entries)
	for _, scheme := range []string{"http", "https"} {
		for _, port := range []string{"", unusedPort(entries)} {
			root := gitURL{scheme: scheme, host: strings.ToLower(host), port: port, path: "/"}
			names = append(names, effectiveHeaders(entries, root)...)
			names = append(names, effectiveHeaders(entries, root.below(depth))...)
		}
	}
	return names
}

// unusedPort returns a port number that no http.<url>.* pattern of entries
// names, so that only a generic header and a pattern without a port apply.
func unusedPort(entries []ConfigEntry) string {
	used := map[string]bool{}
	for _, e := range entries {
		_, sub, _, ok := splitKey(e.Key)
		if !ok || sub == "" {
			continue
		}
		if pattern, ok := parseGitURL(sub, true); ok {
			used[pattern.port] = true
		}
	}
	for n := 1; ; n++ {
		if p := strconv.Itoa(n); !used[p] {
			return p
		}
	}
}

// deepSegmentLen returns a length that exceeds the normalized path of every
// pattern in entries: an escaped byte takes three bytes.
func deepSegmentLen(entries []ConfigEntry) int {
	longest := 0
	for _, e := range entries {
		longest = max(longest, len(e.Key))
	}
	return 3*longest + 1
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
