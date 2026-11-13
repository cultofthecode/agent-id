// Package credential loads the private key that signs a profile's App
// credentials. It knows no provider and never parses the profile config: it
// receives a key reference and returns the checked PEM bytes, or an error that names
// the file and the fix and never contains key material.
//
// The only source is a protected file (File). A keyring backend is OPEN in
// ARCHITECTURE.md and arrives later as another Source.
package credential

import "errors"

// Source loads one private key. PrivateKeyPEM returns the PEM bytes of exactly
// one unencrypted RSA key that has already been parsed and checked; the caller
// owns the slice and clears it once the key is parsed (the GitHub adapter's
// New takes these bytes, github-provider-adapter spec). Every implementation
// fails closed: on any doubt it returns an error and no bytes.
type Source interface {
	PrivateKeyPEM() ([]byte, error)
}

var (
	// ErrInvalidReference reports a key reference that does not name an
	// absolute path or a path under the home directory.
	ErrInvalidReference = errors.New("unsupported private key reference")
	// ErrKeyNotFound reports a key file that does not exist, including a
	// symlink whose target does not exist.
	ErrKeyNotFound = errors.New("private key file not found")
	// ErrKeyUnreadable reports a key file that exists but cannot be opened or
	// read.
	ErrKeyUnreadable = errors.New("private key file not readable")
	// ErrInsecureKeyFile reports a key file that is not a regular file, is
	// accessible by group or others, or is not owned by the current user.
	ErrInsecureKeyFile = errors.New("insecure private key file")
	// ErrInvalidKey reports file content that is not exactly one unencrypted
	// RSA private key in PKCS#1 or PKCS#8 PEM form.
	ErrInvalidKey = errors.New("not a usable RSA private key")
)
