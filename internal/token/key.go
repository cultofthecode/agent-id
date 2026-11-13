package token

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
)

// ScopeFull is the scope of a token that carries the whole grant of its
// subject, the only scope of the MVP. Scoped tokens are Phase 2.
const ScopeFull = "full"

// keyFieldPattern is the shape of every field of a Key.
var keyFieldPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Key identifies the tokens that are interchangeable: those of one provider,
// host, token subject, and scope. It holds no secret.
type Key struct {
	Provider string
	Host     string
	Subject  string
	Scope    string
}

// KeyFor returns the cache key of src's tokens, or ErrInvalidKey when src
// reports a field that does not match ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$.
func KeyFor(src Source) (Key, error) {
	key := Key{Provider: src.Name(), Host: src.Host(), Subject: src.TokenSubject(), Scope: ScopeFull}
	for _, field := range []struct{ name, value string }{
		{"provider", key.Provider}, {"host", key.Host}, {"token subject", key.Subject}, {"scope", key.Scope},
	} {
		if !keyFieldPattern.MatchString(field.value) {
			return Key{}, fmt.Errorf("%w: the %s %q must be 1–128 letters, digits, '.', '_' or '-', starting with a letter or digit", ErrInvalidKey, field.name, field.value)
		}
	}
	return key, nil
}

// String implements fmt.Stringer: provider/host/subject/scope.
func (k Key) String() string {
	return k.Provider + "/" + k.Host + "/" + k.Subject + "/" + k.Scope
}

// fileName returns the name of the key's entry: the hex SHA-256 of the
// canonical encoding, so that no key byte reaches a path.
func (k Key) fileName() string {
	sum := sha256.Sum256([]byte("token-cache-v1\x00" + k.Provider + "\x00" + k.Host + "\x00" + k.Subject + "\x00" + k.Scope + "\x00"))
	return hex.EncodeToString(sum[:]) + entrySuffix
}

// Dir returns the token cache directory that getenv describes:
// $XDG_CACHE_HOME/agent-id/tokens when XDG_CACHE_HOME is absolute, otherwise
// $HOME/.cache/agent-id/tokens, on macOS and Linux alike.
func Dir(getenv func(string) string) (string, error) {
	if base := getenv("XDG_CACHE_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "agent-id", "tokens"), nil
	}
	if home := getenv("HOME"); filepath.IsAbs(home) {
		return filepath.Join(home, ".cache", "agent-id", "tokens"), nil
	}
	return "", fmt.Errorf("%w: set HOME or XDG_CACHE_HOME to an absolute path", ErrNoCacheDir)
}
