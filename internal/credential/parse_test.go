package credential

import (
	"bytes"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
)

func TestParsePrivateKeyAcceptsRSA(t *testing.T) {
	cases := map[string]func(*testing.T) []byte{
		"PKCS#1":                 pkcs1PEM,
		"PKCS#8":                 pkcs8PEM,
		"CRLF line endings":      func(t *testing.T) []byte { return bytes.ReplaceAll(pkcs1PEM(t), []byte("\n"), []byte("\r\n")) },
		"surrounding whitespace": func(t *testing.T) []byte { return append(append([]byte("\n  \n"), pkcs1PEM(t)...), "\n\n"...) },
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			key, err := parsePrivateKey(input(t))
			if err != nil {
				t.Fatalf("parsePrivateKey: %v", err)
			}
			assertKeyMatches(t, key)
		})
	}
}

func TestParsePrivateKeyRejectsHostileInput(t *testing.T) {
	cases := map[string]struct {
		input  func(*testing.T) []byte
		reason string
	}{
		"empty":             {func(*testing.T) []byte { return nil }, "does not start with a PEM block"},
		"whitespace only":   {func(*testing.T) []byte { return []byte(" \n\t\n") }, "does not start with a PEM block"},
		"plain text":        {func(*testing.T) []byte { return []byte("hello\n") }, "does not start with a PEM block"},
		"token":             {func(*testing.T) []byte { return []byte("ghs_aidtest_notakey\n") }, "does not start with a PEM block"},
		"text before block": {func(t *testing.T) []byte { return append([]byte("Bag Attributes\n"), pkcs1PEM(t)...) }, "does not start with a PEM block"},
		"truncated block": {func(t *testing.T) []byte {
			full := pkcs1PEM(t)
			return full[:len(full)/2]
		}, "no complete PEM block"},
		"two keys":           {func(t *testing.T) []byte { return append(pkcs1PEM(t), pkcs8PEM(t)...) }, "exactly one key"},
		"trailing data":      {func(t *testing.T) []byte { return append(pkcs1PEM(t), "garbage\n"...) }, "exactly one key"},
		"certificate":        {blockOf("CERTIFICATE", []byte{0x30, 0x00}), "unsupported PEM block"},
		"public key":         {blockOf("PUBLIC KEY", []byte{0x30, 0x00}), "unsupported PEM block"},
		"EC private key":     {blockOf("EC PRIVATE KEY", []byte{0x30, 0x00}), "unsupported PEM block"},
		"OpenSSH key":        {blockOf("OPENSSH PRIVATE KEY", []byte("openssh-key-v1")), "unsupported PEM block"},
		"PKCS#8 ECDSA":       {ecdsaPKCS8PEM, "not an RSA key"},
		"PKCS#8 Ed25519":     {ed25519PKCS8PEM, "not an RSA key"},
		"encrypted PKCS#8":   {blockOf("ENCRYPTED PRIVATE KEY", []byte{0x30, 0x00}), "encrypted"},
		"encrypted PKCS#1":   {encryptedPKCS1, "encrypted"},
		"other headers":      {withHeader("Comment", "x"), "unexpected PEM headers"},
		"garbage PKCS#1 DER": {blockOf("RSA PRIVATE KEY", []byte{0x30, 0x03, 0x02, 0x01, 0x00}), "PKCS#1 RSA key cannot be parsed"},
		"garbage PKCS#8 DER": {blockOf("PRIVATE KEY", []byte{0x30, 0x03, 0x02, 0x01, 0x00}), "PKCS#8 key cannot be parsed"},
		"PKCS#8 DER labelled PKCS#1": {func(t *testing.T) []byte {
			block, _ := pem.Decode(pkcs8PEM(t))
			return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: block.Bytes})
		}, "PKCS#1 RSA key cannot be parsed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			key, err := parsePrivateKey(tc.input(t))
			if key != nil {
				t.Fatal("parsePrivateKey returned a key for hostile input")
			}
			if !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("error = %v, want ErrInvalidKey", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.reason)
			}
			assertNoKeyMaterial(t, err)
		})
	}
}

func blockOf(blockType string, der []byte) func(*testing.T) []byte {
	return func(*testing.T) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	}
}

func encryptedPKCS1(t *testing.T) []byte {
	return withHeaders(t, map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,00112233445566778899AABBCCDDEEFF"})
}

func withHeader(name, value string) func(*testing.T) []byte {
	return func(t *testing.T) []byte { return withHeaders(t, map[string]string{name: value}) }
}

func withHeaders(t *testing.T, headers map[string]string) []byte {
	t.Helper()
	block, _ := pem.Decode(pkcs1PEM(t))
	block.Headers = headers
	return pem.EncodeToMemory(block)
}
