package activation

import (
	"fmt"
	"strings"
)

// gitURL is a URL normalized as Git's urlmatch does for http.<url>.* keys.
// The path is the raw rest of the URL with percent-encoding normalized, never
// decoded: a query and a fragment stay in it, as in Git.
type gitURL struct {
	scheme, user, host, port, path string
	hasUser                        bool
}

// atHost returns u with its host replaced by host (a plain host name). The
// host keeps a trailing dot, which counts in Git's ranking by host length.
func (u gitURL) atHost(host string) gitURL {
	u.host = strings.ToLower(host)
	return u
}

// parseGitURL normalizes raw by the rules of Git's url_normalize: lower-case
// scheme and host, the password dropped, the default port dropped, percent
// escapes upper-cased and decoded only for unreserved characters, unsafe bytes
// escaped, and dot segments resolved. A host wildcard is allowed only when
// glob is set, as in a pattern. It returns false for a URL that Git rejects
// and also for any form whose result agent-id cannot predict (more than one
// "@", a port with a leading zero, a dot segment at the end of the path), so
// that the caller can fail closed.
func parseGitURL(raw string, glob bool) (gitURL, bool) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || !validScheme(scheme) {
		return gitURL{}, false
	}
	u := gitURL{scheme: strings.ToLower(scheme)}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority, path := rest[:end], rest[end:]
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	hostport, ok := u.splitUser(authority)
	if !ok || !u.splitHostPort(hostport, glob) {
		return gitURL{}, false
	}
	u.path, ok = normalizePath(path)
	return u, ok
}

func validScheme(s string) bool {
	if s == "" || !isAlpha(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isAlpha(c) && !isDigit(c) && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (u *gitURL) splitUser(authority string) (string, bool) {
	userinfo, hostport, found := strings.Cut(authority, "@")
	if !found {
		return authority, true
	}
	if strings.Contains(hostport, "@") {
		return "", false
	}
	user, _, _ := strings.Cut(userinfo, ":")
	var ok bool
	u.hasUser = true
	u.user, ok = escape(user)
	return hostport, ok
}

func (u *gitURL) splitHostPort(hostport string, glob bool) bool {
	host, port := hostport, ""
	if strings.HasPrefix(hostport, "[") {
		end := strings.Index(hostport, "]")
		if end < 0 {
			return false
		}
		host = hostport[:end+1]
		switch tail := hostport[end+1:]; {
		case tail == "":
		case tail[0] == ':':
			port = tail[1:]
		default:
			return false
		}
	} else if i := strings.LastIndex(hostport, ":"); i >= 0 {
		host, port = hostport[:i], hostport[i+1:]
	}
	if !validHost(host, glob) || !validPort(port) {
		return false
	}
	u.host = strings.ToLower(host)
	u.port = port
	if (u.scheme == "http" && port == "80") || (u.scheme == "https" && port == "443") {
		u.port = ""
	}
	return true
}

func validHost(h string, glob bool) bool {
	if strings.HasPrefix(h, "[") {
		return len(h) > 2 && strings.HasSuffix(h, "]") && !strings.ContainsAny(h[1:len(h)-1], "[]%/@ ")
	}
	if h == "" {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if isAlpha(c) || isDigit(c) || c == '.' || c == '-' || c == '_' || (glob && c == '*') {
			continue
		}
		return false
	}
	return true
}

func validPort(p string) bool {
	if p == "" {
		return true
	}
	if len(p) > 5 || (len(p) > 1 && p[0] == '0') {
		return false
	}
	n := 0
	for i := 0; i < len(p); i++ {
		if !isDigit(p[i]) {
			return false
		}
		n = n*10 + int(p[i]-'0')
	}
	return n <= 65535
}

// normalizePath escapes path as Git does and resolves "." and ".." segments
// in the part before the first "?" or "#". Git copies a query and a fragment
// unchanged.
func normalizePath(path string) (string, bool) {
	esc, ok := escape(path)
	if !ok {
		return "", false
	}
	head, tail := esc, ""
	if i := strings.IndexAny(esc, "?#"); i >= 0 {
		head, tail = esc[:i], esc[i:]
	}
	resolved, ok := resolveDots(head)
	return resolved + tail, ok
}

func resolveDots(path string) (string, bool) {
	segments := strings.Split(path, "/")
	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		last := i == len(segments)-1
		switch seg {
		case ".", "..":
			if last {
				return "", false
			}
			if seg == ".." {
				if len(out) <= 1 {
					return "", false
				}
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/"), true
}

const reservedBytes = ":/?#[]@!$&'()*+,;="

// escape normalizes the percent-encoding of s: an unreserved character that
// is escaped becomes literal, any other escape gets upper-case hex digits, and
// a byte that is neither unreserved nor reserved is escaped. A malformed
// escape returns false.
func escape(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' {
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return "", false
			}
			c = unhex(s[i+1])<<4 | unhex(s[i+2])
			i += 2
			writeByte(&b, c, false)
			continue
		}
		writeByte(&b, c, true)
	}
	return b.String(), true
}

func writeByte(b *strings.Builder, c byte, literalReserved bool) {
	switch {
	case isAlpha(c), isDigit(c), c == '-', c == '.', c == '_', c == '~':
		b.WriteByte(c)
	case literalReserved && strings.IndexByte(reservedBytes, c) >= 0:
		b.WriteByte(c)
	default:
		fmt.Fprintf(b, "%%%02X", c)
	}
}

func isHex(c byte) bool { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func unhex(c byte) byte {
	if isDigit(c) {
		return c - '0'
	}
	return c | 0x20 - 'a' + 10
}
