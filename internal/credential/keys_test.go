package credential

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"sync"
	"testing"
)

var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

func pkcs1PEM(t *testing.T) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey())})
}

func pkcs8PEM(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(testKey())
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func ecdsaPKCS8PEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func ed25519PKCS8PEM(t *testing.T) []byte {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func assertNoKeyMaterial(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	message := err.Error()
	if strings.Contains(message, "-----BEGIN") || strings.Contains(message, "-----END") {
		t.Fatalf("error contains a PEM boundary: %q", message)
	}
	for _, encoded := range [][]byte{pkcs1PEM(t), pkcs8PEM(t)} {
		for _, line := range strings.Split(string(encoded), "\n") {
			if len(line) >= 16 && !strings.HasPrefix(line, "-----") && strings.Contains(message, line[:16]) {
				t.Fatalf("error contains key material")
			}
		}
	}
}

func assertKeyMatches(t *testing.T, key *rsa.PrivateKey) {
	t.Helper()
	if key == nil || !key.Equal(testKey()) {
		t.Fatal("loaded key does not match the generated test key")
	}
}
