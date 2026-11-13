package profile

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Classes of failure of the profile configuration. Every error of this
// package and of a provider's SettingsValidator wraps exactly one of them, so
// callers match them with errors.Is. Their own text never appears in a
// message: the message names the file, the field path, and the fix.
var (
	ErrConfigNotFound          = errors.New("profile: configuration not found")
	ErrConfigInvalid           = errors.New("profile: configuration is invalid")
	ErrUnsupportedVersion      = errors.New("profile: unsupported configuration version")
	ErrUnsupportedProvider     = errors.New("profile: unsupported provider")
	ErrUnsupportedHost         = errors.New("profile: unsupported host")
	ErrUnsupportedKeyReference = errors.New("profile: unsupported key reference")
	ErrInlineSecret            = errors.New("profile: configuration contains a secret")
	ErrInsecurePermissions     = errors.New("profile: configuration has insecure permissions")
	ErrProfileNotFound         = errors.New("profile: profile not found")
)

// errMissing marks the ErrConfigNotFound of a file that does not exist. It
// is unexported, so that only the store itself can say a file is missing and
// a provider's error can never make Write start from an empty configuration.
var errMissing = errors.New("profile: no configuration file")

// maxEchoLength is the number of characters of a value that a message may
// echo.
const maxEchoLength = 64

// configError is a failure whose text is a message for the user and whose
// classes are its sentinels.
type configError struct {
	sentinels []error
	message   string
}

func (e *configError) Error() string { return e.message }

func (e *configError) Unwrap() []error { return e.sentinels }

// Errorf returns an error of class sentinel whose text is the formatted
// message alone. A provider's SettingsValidator uses it so that its errors
// match the core sentinels without repeating their text; sentinel must be one
// of the Err variables of this package. A validator may use only
// ErrConfigInvalid, ErrUnsupportedHost, ErrUnsupportedKeyReference, and
// ErrInlineSecret; the core reports any other class as ErrConfigInvalid.
// Echo a value from the file only through Quote.
func Errorf(sentinel error, format string, args ...any) error {
	return &configError{sentinels: []error{sentinel}, message: fmt.Sprintf(format, args...)}
}

// Quote returns s as a double-quoted value that is safe to echo in a
// message: control and format characters, quotes, backslashes, and invalid
// UTF-8 become "?", and the result
// holds at most 64 characters of s. It is meant for identifiers that the user
// needs to see, such as a host or a profile name, never for a secret.
func Quote(s string) string {
	return `"` + sanitize(s) + `"`
}

// sanitize replaces control and format characters (such as bidirectional
// overrides), quotes, backslashes, and invalid UTF-8 in s with "?" and
// cuts it to maxEchoLength characters, marking a cut with "…".
func sanitize(s string) string {
	var b strings.Builder
	count := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if count == maxEchoLength {
			b.WriteString("…")
			break
		}
		if r == utf8.RuneError && size <= 1 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '"' || r == '\\' {
			r = '?'
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

// blockSentinels are the classes a provider's validator may report.
var blockSentinels = []error{ErrConfigInvalid, ErrUnsupportedHost, ErrUnsupportedKeyReference, ErrInlineSecret}

// blockSentinelOf returns the first of blockSentinels that err wraps, or
// ErrConfigInvalid.
func blockSentinelOf(err error) error {
	for _, sentinel := range blockSentinels {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return ErrConfigInvalid
}
