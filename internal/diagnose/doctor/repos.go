package doctor

import (
	"net/url"
	"regexp"
	"strings"
)

// scpLike is the scp-like SSH form "user@host:owner/name".
var scpLike = regexp.MustCompile(`^[^@/\s]+@([^:/\s]+):([^/\s]+/[^/\s]+)$`)

// repositoriesOn returns, without duplicates and without credentials, the
// "owner/name" of each URL that names a repository on host. A URL with
// path with more or fewer than two segments, or another host names none.
// User-info is dropped, never kept.
func repositoriesOn(urls []string, host string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range urls {
		repo, ok := repositoryOn(raw, host)
		if ok && !seen[strings.ToLower(repo)] {
			seen[strings.ToLower(repo)] = true
			out = append(out, repo)
		}
	}
	return out
}

// repositoryOn returns the "owner/name" that raw names on host.
func repositoryOn(raw, host string) (string, bool) {
	if m := scpLike.FindStringSubmatch(raw); m != nil {
		return trimGit(m[2]), strings.EqualFold(m[1], host)
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), host) {
		return "", false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", false
	}
	return trimGit(segments[0] + "/" + segments[1]), true
}

// trimGit removes a trailing ".git".
func trimGit(repo string) string {
	return strings.TrimSuffix(repo, ".git")
}
