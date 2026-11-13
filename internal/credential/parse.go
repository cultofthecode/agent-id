package credential

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

const (
	pkcs1BlockType = "RSA PRIVATE KEY"
	pkcs8BlockType = "PRIVATE KEY"
	pemBegin       = "-----BEGIN "
)

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte(pemBegin)) {
		return nil, invalidKey("the file does not start with a PEM block")
	}
	if bytes.Count(data, []byte(pemBegin)) != 1 {
		return nil, invalidKey("more than one PEM block; the file must hold exactly one key")
	}
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, invalidKey("no complete PEM block found")
	}
	defer clear(block.Bytes)
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, invalidKey("unexpected data after the first PEM block; the file must hold exactly one key")
	}
	if err := checkBlockHeaders(block); err != nil {
		return nil, err
	}
	switch block.Type {
	case pkcs1BlockType:
		return parsePKCS1(block.Bytes)
	case pkcs8BlockType:
		return parsePKCS8(block.Bytes)
	case "ENCRYPTED PRIVATE KEY":
		return nil, invalidKey("the key is encrypted; use the unencrypted PEM file")
	default:
		return nil, invalidKey("unsupported PEM block; expected " + pkcs1BlockType + " (PKCS#1) or " + pkcs8BlockType + " (PKCS#8)")
	}
}

func checkBlockHeaders(block *pem.Block) error {
	if strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
		return invalidKey("the key is encrypted; use the unencrypted PEM file")
	}
	if len(block.Headers) != 0 {
		return invalidKey("unexpected PEM headers")
	}
	return nil
}

func parsePKCS1(der []byte) (*rsa.PrivateKey, error) {
	key, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, invalidKey("the PKCS#1 RSA key cannot be parsed")
	}
	return key, nil
}

func parsePKCS8(der []byte) (*rsa.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, invalidKey("the PKCS#8 key cannot be parsed")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, invalidKey("the PKCS#8 key is not an RSA key")
	}
	return key, nil
}

func invalidKey(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidKey, reason)
}
