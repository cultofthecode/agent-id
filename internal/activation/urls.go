package activation

import (
	"net/url"
	"regexp"
	"strings"
)

// URLKind is how a Git remote URL reaches its host.
type URLKind int

// Kinds of remote URL.
const (
	// KindOther is a URL that is neither SSH nor HTTP(S): a local path,
	// file://, or git://.
	KindOther URLKind = iota
	// KindHTTP is http:// or https://.
	KindHTTP
	// KindSCP is the scp form [user@]host:path.
	KindSCP
	// KindSSH is ssh://, git+ssh://, or ssh+git://.
	KindSSH
)

// scpForm matches [user@]host:path where no slash comes before the colon.
var scpForm = regexp.MustCompile(`^(?:[^/@:]+@)?(\[[^\]]+\]|[^/:@]+):`)

// RemoteURL is a classified remote URL.
type RemoteURL struct {
	Kind URLKind
	// Host is the host name in lower case, without user-info or port.
	Host string
	// Provider reports whether Host is one of the provider's Git hosts.
	Provider bool
}

// ClassifyRemoteURL classifies raw for the provider's Git hosts. It is pure.
func ClassifyRemoteURL(raw string, gitHosts []string) RemoteURL {
	out := RemoteURL{}
	switch scheme, rest, hasScheme := strings.Cut(raw, "://"); {
	case hasScheme && isSSHScheme(scheme):
		out.Kind, out.Host = KindSSH, hostOf(rest)
	case hasScheme && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")):
		out.Kind, out.Host = KindHTTP, hostOf(rest)
	case hasScheme:
		out.Kind = KindOther
	default:
		if m := scpForm.FindStringSubmatch(raw); m != nil {
			out.Kind, out.Host = KindSCP, strings.TrimRight(strings.ToLower(strings.Trim(m[1], "[]")), ".")
		}
	}
	for _, h := range gitHosts {
		if out.Host != "" && strings.EqualFold(h, out.Host) {
			out.Provider = true
		}
	}
	return out
}

func isSSHScheme(s string) bool {
	switch strings.ToLower(s) {
	case "ssh", "git+ssh", "ssh+git":
		return true
	}
	return false
}

// hostOf returns the lower-case host of the authority that starts rest.
func hostOf(rest string) string {
	authority, _, _ := strings.Cut(rest, "/")
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		authority = authority[i+1:]
	}
	if strings.HasPrefix(authority, "[") {
		if end := strings.Index(authority, "]"); end > 0 {
			return strings.ToLower(authority[1:end])
		}
	}
	host, _, _ := strings.Cut(authority, ":")
	return strings.TrimRight(strings.ToLower(host), ".")
}

// HasPassword reports whether an http(s) URL carries a password in its
// user-info. It never returns the password.
func HasPassword(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		_, rest, ok := strings.Cut(raw, "://")
		if !ok {
			return false
		}
		authority, _, _ := strings.Cut(rest, "/")
		at := strings.LastIndex(authority, "@")
		return at >= 0 && strings.Contains(authority[:at], ":")
	}
	if u.User == nil {
		return false
	}
	_, has := u.User.Password()
	return has
}

// Redact returns raw with the user-info replaced by [redacted].
func Redact(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return clean(raw, 64)
	}
	authority, path, _ := strings.Cut(rest, "/")
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = "[redacted]@" + authority[at+1:]
	}
	return clean(scheme+"://"+authority+"/"+path, 64)
}

// aliasName is the form of a host that agent-id passes to ssh -G.
var aliasName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validAlias reports whether host can be passed to ssh -G as an alias.
func validAlias(host string) bool {
	return aliasName.MatchString(host) && !strings.HasPrefix(host, "-")
}
