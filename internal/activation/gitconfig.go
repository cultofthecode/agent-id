package activation

import (
	"strings"
)

// ConfigEntry is one entry of git config --list -z --show-origin --show-scope.
type ConfigEntry struct {
	Scope, Origin, Key, Value string
}

// ParseConfigList parses the output of
// `git config --list -z --show-origin --show-scope`: scope NUL origin NUL
// key LF value NUL, repeated. A key without a value (a bare key) has an
// empty value.
func ParseConfigList(out []byte) []ConfigEntry {
	fields := strings.Split(string(out), "\x00")
	var entries []ConfigEntry
	for i := 0; i+2 < len(fields); i += 3 {
		key, value, _ := strings.Cut(fields[i+2], "\n")
		entries = append(entries, ConfigEntry{Scope: fields[i], Origin: fields[i+1], Key: key, Value: value})
	}
	return entries
}

// source describes where an entry came from, for a message: the scope and
// the file, never the value. Git names the origin of a command-line entry
// "command line:", so the scope would only repeat it.
func (e ConfigEntry) source() string {
	if e.Scope == "command" && strings.HasPrefix(e.Origin, "command line") {
		return clean(e.Origin, 120)
	}
	return clean(e.Scope, 16) + " " + clean(e.Origin, 120)
}

// subsection splits key into its section, subsection, and variable. A key
// without a subsection has an empty subsection.
func splitKey(key string) (section, subsection, variable string, ok bool) {
	first := strings.Index(key, ".")
	last := strings.LastIndex(key, ".")
	if first < 0 {
		return "", "", "", false
	}
	if first == last {
		return key[:first], "", key[first+1:], true
	}
	return key[:first], key[first+1 : last], key[last+1:], true
}

// rule is one insteadOf or pushInsteadOf entry: a URL prefix and its
// replacement.
type rule struct {
	prefix, replacement string
	entry               ConfigEntry
}

// rulesOf returns the entries of variable (insteadof or pushinsteadof) in
// order. In `url.<replacement>.insteadOf = <prefix>` the subsection is the
// replacement and the value is the prefix.
func rulesOf(entries []ConfigEntry, variable string) []rule {
	var rules []rule
	for _, e := range entries {
		section, sub, v, ok := splitKey(e.Key)
		if ok && strings.EqualFold(section, "url") && strings.EqualFold(v, variable) && sub != "" && e.Value != "" {
			rules = append(rules, rule{prefix: strings.ToLower(e.Value), replacement: sub, entry: e})
		}
	}
	return rules
}

// overlap finds, for the base URL, the longest rule whose prefix is a prefix
// of base, and every rule whose prefix lies under base (a longer prefix).
func overlap(rules []rule, base string) (best *rule, deeper []rule) {
	base = strings.ToLower(base)
	for i := range rules {
		r := rules[i]
		switch {
		case strings.HasPrefix(base, r.prefix):
			if best == nil || len(r.prefix) >= len(best.prefix) {
				best = &rules[i]
			}
		case strings.HasPrefix(r.prefix, base):
			deeper = append(deeper, r)
		}
	}
	return best, deeper
}

// rewritesToSSH reports whether a rule's replacement is an SSH URL: the
// rewritten URL would use the developer's SSH key.
func rewritesToSSH(r rule) bool {
	switch k := ClassifyRemoteURL(r.replacement, nil).Kind; k {
	case KindSSH, KindSCP:
		return true
	}
	return false
}

// RewritesToSSH evaluates, as Git does, whether the url.*.insteadOf and
// pushInsteadOf rules turn base, or any path under it, into an SSH URL. Fetch
// uses the longest matching insteadOf; push uses the longest matching
// pushInsteadOf and falls back to insteadOf only when none matches. It
// returns the first offending entry.
func RewritesToSSH(entries []ConfigEntry, base string) (ConfigEntry, bool) {
	instead := rulesOf(entries, "insteadof")
	pushInstead := rulesOf(entries, "pushinsteadof")
	fetchBest, fetchDeeper := overlap(instead, base)
	pushBest, pushDeeper := overlap(pushInstead, base)
	if fetchBest != nil && rewritesToSSH(*fetchBest) {
		return fetchBest.entry, true
	}
	effectivePush := pushBest
	if effectivePush == nil {
		effectivePush = fetchBest
	}
	if effectivePush != nil && rewritesToSSH(*effectivePush) {
		return effectivePush.entry, true
	}
	for _, r := range append(fetchDeeper, pushDeeper...) {
		if rewritesToSSH(r) {
			return r.entry, true
		}
	}
	return ConfigEntry{}, false
}

// RuleFor returns the rule that rewrites raw (a configured remote URL) for
// fetch or push, for naming its source in a message.
func RuleFor(entries []ConfigEntry, raw string, push bool) (ConfigEntry, bool) {
	instead := rulesOf(entries, "insteadof")
	var pushRules []rule
	if push {
		pushRules = rulesOf(entries, "pushinsteadof")
	}
	best := longest(pushRules, raw)
	if best == nil {
		best = longest(instead, raw)
	}
	if best == nil {
		return ConfigEntry{}, false
	}
	return best.entry, true
}

func longest(rules []rule, raw string) *rule {
	var best *rule
	lower := strings.ToLower(raw)
	for i := range rules {
		if strings.HasPrefix(lower, rules[i].prefix) && (best == nil || len(rules[i].prefix) >= len(best.prefix)) {
			best = &rules[i]
		}
	}
	return best
}
