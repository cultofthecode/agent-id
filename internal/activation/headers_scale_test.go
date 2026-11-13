package activation

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// referenceEffectiveHeaders is the evaluation that EffectiveHeaders had before
// #108: every entry is parsed again for every URL. The differential tests
// compare the indexed evaluation with it.
func referenceEffectiveHeaders(entries []ConfigEntry, target gitURL) []string {
	var headers []string
	var best specificity
	seen := false
	for _, e := range entries {
		spec, ok := referenceHeaderScope(e.Key, target)
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

func referenceHeaderScope(key string, target gitURL) (*specificity, bool) {
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

func referenceScopedHeaders(entries []ConfigEntry, hosts []string) []HostHeader {
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
				for _, name := range referenceEffectiveHeaders(entries, target) {
					if hh := (HostHeader{Host: h, Name: name}); !slices.Contains(out, hh) {
						out = append(out, hh)
					}
				}
			}
		}
	}
	return out
}

func randomEntries(rng *rand.Rand, n int) []ConfigEntry {
	users := []string{"", "@", "u@", "v@", ":p@"}
	hosts := []string{"github.com", "github.com.", "GitHub.com:443", "*.github.com", "gist.github.com", "*.github.*", "*.com", "example.org", "github.com:8080", "[::1]", "bad host"}
	paths := []string{"", "/", "/org/", "/org//", "/org", "/org/x/", "/org/x?/../", "/org/./", "/%6Frg/", "//", "/a/b/c/", "/org/x?"}
	schemes := []string{"https", "https", "http", "ftp", "bad_scheme"}
	var out []ConfigEntry
	for range n {
		key := "http.extraheader"
		if rng.IntN(6) != 0 {
			key = "http." + schemes[rng.IntN(len(schemes))] + "://" + users[rng.IntN(len(users))] + hosts[rng.IntN(len(hosts))] + paths[rng.IntN(len(paths))] + ".extraheader"
		}
		if rng.IntN(25) == 0 {
			key = "core.sshcommand"
		}
		value := []string{"X-L: 1", "X-M: 2", "", "", "no colon"}[rng.IntN(5)]
		out = append(out, ConfigEntry{Scope: "global", Origin: "file:/h/.gitconfig", Key: key, Value: value})
	}
	return out
}

func TestIndexedHeadersAgreeWithTheReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	hosts := []string{"github.com", "gist.github.com", "ssh.github.com", "GitHub.com.", "example.org", "[::1]"}
	targets := []string{"https://github.com/", "https://github.com/org/r", "https://github.com/org/x/r", "http://github.com/", "http://gist.github.com/org/",
		"https://u@github.com/org/r", "https://v@gist.github.com/", "https://github.com:8080/org/", "http://example.org/a/b/c/d", "https://[::1]/", "https://github.com/%6Frg/r"}
	for i := range 3000 {
		es := randomEntries(rng, 1+rng.IntN(8))
		for _, raw := range targets {
			target, ok := parseGitURL(raw, false)
			if !ok {
				t.Fatalf("target %q does not parse", raw)
			}
			want, got := referenceEffectiveHeaders(es, target), EffectiveHeaders(es, raw)
			if !slices.Equal(want, got) {
				t.Fatalf("set %d, URL %q, entries %q: reference %q, indexed %q", i, raw, es, want, got)
			}
		}
		if want, got := referenceScopedHeaders(es, hosts), ScopedHeaders(es, hosts); !slices.Equal(want, got) {
			t.Fatalf("set %d, entries %q: ScopedHeaders reference %q, indexed %q", i, es, want, got)
		}
		depth := deepSegmentLen(es)
		for _, h := range hosts[:3] {
			var want []string
			for _, scheme := range []string{"http", "https"} {
				for _, port := range []string{"", unusedPort(es)} {
					root := gitURL{scheme: scheme, host: strings.ToLower(h), port: port, path: "/"}
					want = append(want, referenceEffectiveHeaders(es, root)...)
					want = append(want, referenceEffectiveHeaders(es, root.below(depth))...)
				}
			}
			if got := HostRootHeaders(es, h); !slices.Equal(want, got) {
				t.Fatalf("set %d, host %q, entries %q: HostRootHeaders reference %q, indexed %q", i, h, es, want, got)
			}
		}
	}
}

func manyEntries(n int) []ConfigEntry {
	es := entries("http.https://github.com/.extraheader", "", "http.http://github.com/.extraheader", "")
	for i := range n {
		es = append(es, entries(fmt.Sprintf("http.https://github.com/org-%d/.extraheader", i), "X-L: 1")...)
	}
	return es
}

// TestManyHeaderEntriesAreFast is the size test of #108: 1,000 entries took
// 29 s with the first version of the check.
func TestManyHeaderEntriesAreFast(t *testing.T) {
	hosts := []string{"github.com", "gist.github.com", "ssh.github.com"}
	es := manyEntries(1000)
	start := time.Now()
	got := ScopedHeaders(es, hosts)
	root := HostRootHeaders(es, "github.com")
	took := time.Since(start)
	if len(got) != 1 || got[0] != (HostHeader{Host: "github.com", Name: "X-L"}) || len(root) != 0 {
		t.Fatalf("ScopedHeaders = %v, HostRootHeaders = %v", got, root)
	}
	if took > 2*time.Second {
		t.Errorf("1000 entries took %v, want under 2s", took)
	}
	t.Logf("1000 entries: %v", took)
}

func BenchmarkScopedHeaders(b *testing.B) {
	hosts := []string{"github.com", "gist.github.com", "ssh.github.com"}
	for _, n := range []int{100, 1000, 10000} {
		es := manyEntries(n)
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			for range b.N {
				ScopedHeaders(es, hosts)
			}
		})
	}
}

func TestSourceOfACommandLineEntry(t *testing.T) {
	cmd := ConfigEntry{Scope: "command", Origin: "command line:"}
	if got := cmd.source(); got != "command line:" {
		t.Errorf("source = %q", got)
	}
	file := ConfigEntry{Scope: "global", Origin: "file:/h/.gitconfig"}
	if got := file.source(); got != "global file:/h/.gitconfig" {
		t.Errorf("source = %q", got)
	}
}
