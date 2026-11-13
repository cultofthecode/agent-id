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
	return compileHeaderRules(entries).effective(target)
}

// headerRule is one http extraheader entry, parsed once. A generic entry has
// no pattern. An entry whose pattern does not parse is never a rule: it never
// applies, and UnparsableHeaderScope reports it separately.
type headerRule struct {
	sub     string
	generic bool
	pattern gitURL
	labels  []string
	path    string
	value   string
}

// pathKey is what a rule needs to match, apart from its host and user: the
// scheme, the port, and the path of its pattern without a trailing slash.
type pathKey struct{ scheme, port, path string }

// headerIndex holds the rules whose host pattern matches one host, in entry
// order, with the scoped ones grouped by pathKey.
type headerIndex struct {
	rules   []*headerRule
	generic []int
	byPath  map[pathKey][]int
}

// headerRules is the parsed form of the extraheader entries of a
// configuration. It parses each pattern once and builds one headerIndex per
// host, so evaluating a URL looks at the rules that can match it and not at
// every entry.
type headerRules struct {
	rules []headerRule
	index map[string]*headerIndex
}

func compileHeaderRules(entries []ConfigEntry) *headerRules {
	hr := &headerRules{index: map[string]*headerIndex{}}
	for _, e := range entries {
		section, sub, variable, ok := splitKey(e.Key)
		if !ok || !strings.EqualFold(section, "http") || !strings.EqualFold(variable, "extraheader") {
			continue
		}
		if sub == "" {
			hr.rules = append(hr.rules, headerRule{generic: true, value: e.Value})
			continue
		}
		pattern, ok := parseGitURL(sub, true)
		if !ok {
			continue
		}
		hr.rules = append(hr.rules, headerRule{
			sub:     sub,
			pattern: pattern,
			labels:  hostLabels(pattern.host),
			path:    strings.TrimSuffix(pattern.path, "/"),
			value:   e.Value,
		})
	}
	return hr
}

func (hr *headerRules) indexFor(host string) *headerIndex {
	if x, ok := hr.index[host]; ok {
		return x
	}
	labels := hostLabels(host)
	x := &headerIndex{byPath: map[pathKey][]int{}}
	for i := range hr.rules {
		r := &hr.rules[i]
		if !r.generic && !labelsMatch(r.labels, labels) {
			continue
		}
		n := len(x.rules)
		x.rules = append(x.rules, r)
		if r.generic {
			x.generic = append(x.generic, n)
			continue
		}
		k := pathKey{r.pattern.scheme, r.pattern.port, r.path}
		x.byPath[k] = append(x.byPath[k], n)
	}
	hr.index[host] = x
	return x
}

// effective returns the header names for target, in the order and with the
// rules of Git's urlmatch as described at EffectiveHeaders.
func (hr *headerRules) effective(target gitURL) []string {
	x := hr.indexFor(target.host)
	var headers []string
	var best specificity
	seen := false
	for _, i := range x.candidates(target) {
		r := x.rules[i]
		spec, ok := r.specificity(target)
		if !ok {
			continue
		}
		if seen && spec.less(best) && r.value == "" {
			continue
		}
		if !spec.less(best) || !seen {
			best, seen = spec, true
		}
		if r.value == "" {
			headers = nil
			continue
		}
		headers = append(headers, headerName(r.value))
	}
	return headers
}

// candidates returns, in entry order, the rules that match target by scheme,
// port and path: the generic ones and every scoped rule whose path is the
// path of target or one of its ancestors.
func (x *headerIndex) candidates(target gitURL) []int {
	out := slices.Clone(x.generic)
	add := func(path string) {
		out = append(out, x.byPath[pathKey{target.scheme, target.port, path}]...)
	}
	tp := target.path
	add("")
	for i := 1; i < len(tp); i++ {
		if tp[i] == '/' {
			add(tp[:i])
		}
	}
	if tp != "" {
		add(tp)
	}
	slices.Sort(out)
	return out
}

// specificity reports whether r applies to target, whose host the index
// already matched, and how specifically. A generic rule ranks lowest.
func (r *headerRule) specificity(target gitURL) (specificity, bool) {
	if r.generic {
		return specificity{}, true
	}
	user := 0
	if r.pattern.hasUser {
		if !target.hasUser || r.pattern.user != target.user {
			return specificity{}, false
		}
		user = 1
	}
	return specificity{host: len(r.pattern.host), user: user, path: len(r.path)}, true
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
	found := map[HostHeader]bool{}
	seen := map[string]bool{}
	depth := deepSegmentLen(entries)
	rules := compileHeaderRules(entries)
	for i := range rules.rules {
		r := &rules.rules[i]
		if r.generic || seen[r.sub] {
			continue
		}
		seen[r.sub] = true
		for _, h := range hosts {
			if !labelsMatch(r.labels, hostLabels(h)) {
				continue
			}
			at := r.pattern.atHost(h)
			for _, target := range []gitURL{at, at.below(depth)} {
				for _, name := range rules.effective(target) {
					if hh := (HostHeader{Host: h, Name: name}); !found[hh] {
						found[hh] = true
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
	rules := compileHeaderRules(entries)
	depth := deepSegmentLen(entries)
	var names []string
	for _, scheme := range []string{"http", "https"} {
		for _, port := range []string{"", unusedPort(entries)} {
			root := gitURL{scheme: scheme, host: strings.ToLower(host), port: port, path: "/"}
			names = append(names, rules.effective(root)...)
			names = append(names, rules.effective(root.below(depth))...)
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
	return labelsMatch(hostLabels(pattern), hostLabels(host))
}

func hostLabels(host string) []string {
	return strings.Split(strings.ToLower(strings.TrimSuffix(host, ".")), ".")
}

func labelsMatch(pattern, host []string) bool {
	if len(pattern) != len(host) {
		return false
	}
	for i := range pattern {
		if pattern[i] != "*" && pattern[i] != host[i] {
			return false
		}
	}
	return true
}
