package diagnose

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// redacted replaces a secret in a string.
const redacted = "[redacted]"

// maxValue is the longest string, in characters, that a report echoes.
const maxValue = 200

var (
	pemPattern      = regexp.MustCompile(`(?s)-----BEGIN.*?(?:-----END[^\n]*?-----|\z)`)
	jwtPattern      = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	userinfoPattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*)://([^/\s?#]*)@`)
	labelPattern    = regexp.MustCompile(`(?i)((?:authorization|password|oauth_token|access_token)["']?\s*[:=]|token(?:["']\s*:\s*["']|\s*=))([^\r\n]*)`)
	bearerPattern   = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{8,}`)
)

// Patterns is what Redact and Guard look for: the fixed shapes of a PEM
// block, a JWT, URL user-info and credential labels, and the token prefixes
// that the provider adapters registered.
type Patterns struct {
	prefixes []string
	prefixRE *regexp.Regexp
}

// NewPatterns returns the patterns for the token prefixes, normally
// profile.Registry.TokenPrefixes. Empty prefixes are ignored.
func NewPatterns(prefixes []string) Patterns {
	var kept, quoted []string
	for _, p := range prefixes {
		if p != "" {
			kept = append(kept, p)
			quoted = append(quoted, regexp.QuoteMeta(p))
		}
	}
	p := Patterns{prefixes: kept}
	if len(quoted) > 0 {
		p.prefixRE = regexp.MustCompile(`(?:` + strings.Join(quoted, "|") + `)[A-Za-z0-9_]*`)
	}
	return p
}

// Redact removes every secret shape from s, in this order: PEM blocks, JWTs,
// registered token prefixes, URL user-info (the user of an ssh:// URL
// without a password stays; the user-info runs to the last "@" of the
// authority), the values of Authorization, password, oauth_token,
// access_token labels, "token=" and quoted "token": "..." labels (prose such as "token: expired" stays), and bare Bearer tokens; then control characters become "?" and the result is
// cut to 200 characters.
func (p Patterns) Redact(s string) string {
	s = pemPattern.ReplaceAllString(s, redacted)
	s = jwtPattern.ReplaceAllString(s, redacted)
	if p.prefixRE != nil {
		s = p.prefixRE.ReplaceAllString(s, redacted)
	}
	s = userinfoPattern.ReplaceAllStringFunc(s, redactUserinfo)
	s = labelPattern.ReplaceAllString(s, "${1} "+redacted)
	s = bearerPattern.ReplaceAllString(s, "${1} "+redacted)
	return cut(replaceControl(s))
}

// redactUserinfo redacts the user-info of one "scheme://user@" match.
func redactUserinfo(match string) string {
	parts := userinfoPattern.FindStringSubmatch(match)
	scheme, userinfo := parts[1], parts[2]
	if keepsUserinfo(scheme, userinfo) {
		return match
	}
	return scheme + "://" + redacted + "@"
}

// keepsUserinfo reports whether user-info is not a secret: the user of an
// ssh URL with no password, or user-info that is already redacted.
func keepsUserinfo(scheme, userinfo string) bool {
	if userinfo == redacted {
		return true
	}
	return strings.EqualFold(scheme, "ssh") && !strings.Contains(userinfo, ":")
}

// replaceControl turns control characters into "?".
func replaceControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return '?'
		}
		return r
	}, s)
}

// cut limits s to maxValue characters, ending with an ellipsis when it cuts.
func cut(s string) string {
	if utf8.RuneCountInString(s) <= maxValue {
		return s
	}
	return string([]rune(s)[:maxValue-1]) + "…"
}

// Guard scans out, the text that a command is about to write, and returns
// ErrWithheld when it still holds a PEM marker, a JWT, a registered token
// prefix, user-info with a password or a user other than an ssh user, or a
// credential label whose value is not redacted. It uses the same patterns as
// Redact and the same exemptions.
func (p Patterns) Guard(out []byte) error {
	s := string(out)
	switch {
	case strings.Contains(s, "-----BEGIN"), jwtPattern.MatchString(s):
		return ErrWithheld
	case p.prefixRE != nil && p.prefixRE.MatchString(s):
		return ErrWithheld
	case guardUserinfo(s), guardLabels(s), bearerPattern.MatchString(s):
		return ErrWithheld
	}
	return nil
}

// guardUserinfo reports whether s holds user-info that Redact would change.
func guardUserinfo(s string) bool {
	for _, m := range userinfoPattern.FindAllString(s, -1) {
		if redactUserinfo(m) != m {
			return true
		}
	}
	return false
}

// guardLabels reports whether s holds a credential label with a value other
// than the redaction marker. Text that a renderer put after the marker (a
// closing quote, a comma, a source in parentheses) is not the value, but it
// is scanned again, because it can hold another label.
func guardLabels(s string) bool {
	for _, m := range labelPattern.FindAllStringSubmatch(s, -1) {
		rest, ok := afterRedactedValue(strings.TrimSpace(m[2]))
		if !ok || guardLabels(rest) {
			return true
		}
	}
	return false
}

// afterRedactedValue reports whether v is empty or starts with the
// redaction marker followed by the end, a delimiter (Redact shows a line
// break as "?") or a parenthesized note, and returns the text after the
// marker.
func afterRedactedValue(v string) (string, bool) {
	if v == "" {
		return "", true
	}
	rest, ok := strings.CutPrefix(v, redacted)
	if !ok {
		return "", false
	}
	rest = strings.TrimLeft(rest, " ")
	if rest == "" || strings.ContainsRune("\"',;)]}(?", rune(rest[0])) {
		return rest, true
	}
	return "", false
}

// AbbreviateHome replaces the home directory in text by "~", so that a pasted
// report does not carry the user's account name. It replaces home only where
// it is a whole path prefix: "/home/devx" is not under "/home/dev", and
// "/mnt/home/dev" is not "~". An empty, relative, or root home changes
// nothing.
func AbbreviateHome(text, home string) string {
	home = strings.TrimRight(home, "/")
	if home == "" || home[0] != '/' {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], home) && startsPath(text, i) && endsPath(text, i+len(home)) {
			b.WriteByte('~')
			i += len(home)
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

// startsPath reports whether a path may start at text[i].
func startsPath(text string, i int) bool {
	return i == 0 || !isPathByte(text[i-1])
}

// endsPath reports whether a path component ends at text[i].
func endsPath(text string, i int) bool {
	return i == len(text) || text[i] == '/' || !isPathByte(text[i])
}

// isPathByte reports whether b can continue a path name.
func isPathByte(b byte) bool {
	return b == '.' || b == '_' || b == '-' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// Matches reports whether s has the shape of a secret: a PEM block, a JWT, a
// registered token prefix, or credentials in a URL or a label. Gatherers use
// it to report a variable by name without keeping its value.
func (p Patterns) Matches(s string) bool {
	return p.Guard([]byte(s)) != nil
}
