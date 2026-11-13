package profile

import (
	"path/filepath"
	"strings"
	"unicode"
)

// Key reference schemes.
const (
	SchemeFile    = "file"
	SchemeKeyring = "keyring"
)

// homePrefix starts a file reference relative to the user's home directory.
const homePrefix = "~/"

// KeyRef is a reference to a private key, in the form <scheme>:<value>. In
// v1 only the file scheme is accepted: Value is an absolute, cleaned path or
// a path that starts with "~/". The credential source loads the key; a
// KeyRef never holds key material.
type KeyRef struct {
	Scheme string
	Value  string
}

// String returns the reference in the form <scheme>:<value>.
func (r KeyRef) String() string {
	return r.Scheme + ":" + r.Value
}

// ParseKeyRef parses a key reference. It checks the syntax only, never the
// file. Its errors wrap ErrUnsupportedKeyReference, carry no field path (the
// caller prefixes one), and never echo s.
func ParseKeyRef(s string) (KeyRef, error) {
	scheme, value, found := strings.Cut(s, ":")
	if !found || strings.ContainsFunc(s, unicode.IsControl) {
		return KeyRef{}, unknownKeyRef()
	}
	switch scheme {
	case SchemeFile:
		return parseFileRef(value)
	case SchemeKeyring:
		return KeyRef{}, Errorf(ErrUnsupportedKeyReference, "keyring references are reserved and not supported yet; use file:<path>")
	default:
		return KeyRef{}, unknownKeyRef()
	}
}

// parseFileRef parses the value of a file reference.
func parseFileRef(value string) (KeyRef, error) {
	if rest, ok := strings.CutPrefix(value, homePrefix); ok && insideHome(filepath.Clean(rest)) {
		return KeyRef{Scheme: SchemeFile, Value: homePrefix + filepath.Clean(rest)}, nil
	}
	if filepath.IsAbs(value) {
		return KeyRef{Scheme: SchemeFile, Value: filepath.Clean(value)}, nil
	}
	return KeyRef{}, Errorf(ErrUnsupportedKeyReference, "a file reference needs an absolute path or ~/…")
}

// insideHome reports whether the cleaned relative path rest names a file
// below the home directory rather than the directory itself or a path
// outside it.
func insideHome(rest string) bool {
	return rest != "." && rest != ".." && !strings.HasPrefix(rest, "../") && !filepath.IsAbs(rest)
}

// Path returns the file path of a file reference, with a leading "~/"
// expanded to home, which must be an absolute path.
func (r KeyRef) Path(home string) (string, error) {
	if r.Scheme != SchemeFile {
		return "", Errorf(ErrUnsupportedKeyReference, "%s references have no file path", Quote(r.Scheme))
	}
	rest, relative := strings.CutPrefix(r.Value, homePrefix)
	if !relative {
		return r.Value, nil
	}
	if !filepath.IsAbs(home) {
		return "", Errorf(ErrUnsupportedKeyReference, "cannot expand ~/ in a file reference: HOME is not an absolute path")
	}
	return filepath.Join(home, rest), nil
}

// unknownKeyRef is the error for a reference of no known form.
func unknownKeyRef() error {
	return Errorf(ErrUnsupportedKeyReference, "unknown key reference; use file:<path>")
}
