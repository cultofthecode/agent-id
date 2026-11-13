package github

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"regexp"
)

// minKeyBits is the smallest App key the adapter accepts; GitHub issues
// 2048-bit RSA keys.
const minKeyBits = 2048

// parsePrivateKey parses the PEM private key of a GitHub App: exactly one
// unencrypted PKCS#1 or PKCS#8 RSA key of at least minKeyBits, with nothing
// but whitespace around it. Errors describe the problem and never contain
// the input.
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: not a PEM private key", ErrInvalidKey)
	}
	leading := data[:bytes.Index(data, []byte("-----BEGIN"))]
	if len(bytes.TrimSpace(leading)) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: unexpected data around the PEM block", ErrInvalidKey)
	}
	if len(block.Headers) != 0 {
		return nil, fmt.Errorf("%w: encrypted or annotated PEM keys are not supported", ErrInvalidKey)
	}
	key, err := decodeRSAKey(block)
	if err != nil {
		return nil, err
	}
	if key.N.BitLen() < minKeyBits {
		return nil, fmt.Errorf("%w: RSA key of %d bits, want at least %d", ErrInvalidKey, key.N.BitLen(), minKeyBits)
	}
	if key.Validate() != nil {
		return nil, fmt.Errorf("%w: inconsistent RSA key", ErrInvalidKey)
	}
	return key, nil
}

// decodeRSAKey decodes the DER body of block as a PKCS#1 or PKCS#8 RSA key.
func decodeRSAKey(block *pem.Block) (*rsa.PrivateKey, error) {
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed PKCS#1 key", ErrInvalidKey)
		}
		return key, nil
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed PKCS#8 key", ErrInvalidKey)
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%w: not an RSA key", ErrInvalidKey)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("%w: unsupported PEM block %q", ErrInvalidKey, safeBlockType(block.Type))
	}
}

// blockTypePattern is the shape of a PEM block type that an error may name.
var blockTypePattern = regexp.MustCompile(`^[A-Za-z0-9 ]{1,32}$`)

// safeBlockType returns the PEM block type for a message, or "unknown" when
// it is long or holds anything but letters, digits, and spaces.
func safeBlockType(blockType string) string {
	if !blockTypePattern.MatchString(blockType) {
		return "unknown"
	}
	return blockType
}
