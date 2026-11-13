package github

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestParsePrivateKey(t *testing.T) {
	testenv.Isolate(t)
	app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	pkcs1 := app.PrivateKeyPEM
	block, _ := pem.Decode(pkcs1)
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := encodePKCS8(t, rsaKey)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	smallKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-256-CBC,00"}, Bytes: block.Bytes})

	tests := []struct {
		name  string
		input []byte
		ok    bool
	}{
		{"PKCS#1", pkcs1, true},
		{"PKCS#8", pkcs8, true},
		{"surrounding blank lines", append(append([]byte("\n\n"), pkcs1...), "\n\n"...), true},
		{"empty", nil, false},
		{"garbage", []byte("not a key"), false},
		{"two PEM blocks", append(append([]byte{}, pkcs1...), pkcs1...), false},
		{"trailing bytes", append(append([]byte{}, pkcs1...), "trailing"...), false},
		{"leading bytes", append([]byte("leading\n"), pkcs1...), false},
		{"unterminated leading block", append([]byte("-----BEGIN X-----\n"), pkcs1...), false},
		{"encrypted PEM", encrypted, false},
		{"EC key", encodePKCS8(t, ecKey), false},
		{"Ed25519 key", encodePKCS8(t, edKey), false},
		{"1024-bit RSA key", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(smallKey)}), false},
		{"certificate block", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}), false},
		{"hostile block type", pem.EncodeToMemory(&pem.Block{Type: "X\x1b[31m", Bytes: block.Bytes}), false},
		{"corrupt PKCS#1 body", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: block.Bytes[:64]}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parsePrivateKey(tt.input)
			if tt.ok {
				if err != nil || key == nil {
					t.Fatalf("parsePrivateKey: %v, want a key", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidKey) || key != nil {
				t.Fatalf("parsePrivateKey: key %v, error %v, want ErrInvalidKey", key != nil, err)
			}
			checkNoKeyMaterial(t, err.Error(), block.Bytes)
			if strings.ContainsRune(err.Error(), '\x1b') {
				t.Errorf("error %q holds a control character", err)
			}
		})
	}
}

func TestNewChecksTheConfig(t *testing.T) {
	testenv.Isolate(t)
	app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{"zero App ID", Config{InstallationID: 1, PrivateKeyPEM: app.PrivateKeyPEM}, ErrInvalidConfig},
		{"negative installation ID", Config{AppID: 1, InstallationID: -1, PrivateKeyPEM: app.PrivateKeyPEM}, ErrInvalidConfig},
		{"no key", Config{AppID: 1, InstallationID: 1}, ErrInvalidKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); !errors.Is(err, tt.want) {
				t.Errorf("New: %v, want %v", err, tt.want)
			}
		})
	}
	p, err := New(Config{AppID: 1, InstallationID: 2, PrivateKeyPEM: app.PrivateKeyPEM})
	if err != nil {
		t.Fatal(err)
	}
	if p.baseURL != "https://api.github.com" {
		t.Errorf("New uses %q, want https://api.github.com", p.baseURL)
	}
	if p.client.CheckRedirect == nil || p.client.Timeout == 0 {
		t.Error("New: the default client follows redirects or has no timeout")
	}
}

func TestCheckBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://api.github.com", "https://api.github.com"},
		{"https://api.github.com/", "https://api.github.com"},
		{"http://127.0.0.1:18001", "http://127.0.0.1:18001"},
		{"http://[::1]:18001/", "http://[::1]:18001"},
		{"http://api.github.com", ""},
		{"http://localhost:18001", ""},
		{"http://127.0.0.1.evil.test", ""},
		{"https://user:pass@api.github.com", ""},
		{"https://api.github.com/?x=1", ""},
		{"https://api.github.com/?", ""},
		{"https://api.github.com/#x", ""},
		{"ftp://api.github.com", ""},
		{"api.github.com", ""},
		{"https:opaque", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := checkBaseURL(tt.input)
		if tt.want == "" {
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("checkBaseURL(%q) = %q, %v, want ErrInvalidConfig", tt.input, got, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("checkBaseURL(%q) = %q, %v, want %q", tt.input, got, err, tt.want)
		}
	}
}

func TestFormattingRedactsTheKey(t *testing.T) {
	app := newFakeApp(t)
	cfg := Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}
	p := newAdapter(t, app)
	block, _ := pem.Decode(app.PrivateKeyPEM)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, value := range []any{cfg, &cfg, p, *p} {
			text := fmt.Sprintf(verb, value)
			checkNoKeyMaterial(t, text, block.Bytes)
			if !strings.Contains(text, "424242") {
				t.Errorf("formatting %T with %s gives %q, want the App ID", value, verb, text)
			}
		}
	}
}

func TestJSONOmitsTheKey(t *testing.T) {
	app := newFakeApp(t)
	data, err := json.Marshal(Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PrivateKeyPEM") || strings.Contains(string(data), "LS0t") {
		t.Errorf("json.Marshal(Config) = %s, want no key", data)
	}
}

// encodePKCS8 returns key as a PKCS#8 PEM block.
func encodePKCS8(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// checkNoKeyMaterial fails the test when text holds PEM or a fragment of der.
func checkNoKeyMaterial(t *testing.T, text string, der []byte) {
	t.Helper()
	if strings.Contains(text, "BEGIN") || strings.Contains(text, "MII") || strings.Contains(text, string(der[:16])) {
		t.Errorf("%q holds key material", text)
	}
}
