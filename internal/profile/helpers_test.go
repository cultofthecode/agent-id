package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.Main(m)
}

// acmeHost is the only host the fake provider accepts.
const acmeHost = "git.acme.test"

// acmePrefix is the token prefix the fake provider registers.
const acmePrefix = "acm_"

// acmeValidator is a provider-neutral stand-in for an adapter: its block has
// a positive integer "id" and a key reference "key", and it accepts one host.
type acmeValidator struct{}

func (acmeValidator) ValidateSettings(host string, settings json.RawMessage) error {
	if host != acmeHost {
		return Errorf(ErrUnsupportedHost, "host: %s is not supported by the acme provider", Quote(host))
	}
	var block struct {
		ID  json.RawMessage `json:"id"`
		Key string          `json:"key"`
	}
	dec := json.NewDecoder(bytes.NewReader(settings))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&block); err != nil {
		return Errorf(ErrConfigInvalid, "acme: %v", err)
	}
	if !isPositiveInteger(string(block.ID)) {
		return Errorf(ErrConfigInvalid, "acme.id: must be a positive integer")
	}
	if _, err := ParseKeyRef(block.Key); err != nil {
		return fmt.Errorf("acme.key: %w", err)
	}
	return nil
}

// plainValidator fails with an error that wraps no core sentinel.
type plainValidator struct{}

func (plainValidator) ValidateSettings(string, json.RawMessage) error {
	return errors.New("plain: broken")
}

// acmeRegistry returns a registry with the fake provider "acme".
func acmeRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry(Registration{Name: "acme", Validator: acmeValidator{}, TokenPrefixes: []string{acmePrefix}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// exampleConfig is the v1 example of the spec with the fake provider in
// place of GitHub, and a profile of a provider that is not registered.
const exampleConfig = `{
  "version": 1,
  "profiles": {
    "claude": {
      "provider": "acme",
      "host": "git.acme.test",
      "acme": {
        "id": 123456,
        "key": "file:~/.config/agent-id/keys/claude.pem"
      }
    },
    "reviewer": {
      "provider": "acme",
      "host": "git.acme.test",
      "acme": {"id": 456789, "key": "file:/keys/reviewer.pem"}
    },
    "gl-bot": {
      "provider": "gitlab",
      "host": "gitlab.example",
      "gitlab": {"token_ref": "file:/k.pem", "nested": [1, 2.50, {"x": null, "y": true}], "url": "<a&b>"}
    }
  }
}`

// glBotSettings is the provider block of gl-bot in exampleConfig, compact.
const glBotSettings = `{"token_ref":"file:/k.pem","nested":[1,2.50,{"x":null,"y":true}],"url":"<a&b>"}`

// profileDoc returns a v1 document whose profiles object holds profiles.
func profileDoc(profiles string) string {
	return `{"version": 1, "profiles": {` + profiles + `}}`
}

// acmeProfile returns the member of a valid acme profile named name.
func acmeProfile(name string) string {
	return `"` + name + `": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem"}}`
}

// writeFile writes the configuration file of env with mode, creating its
// directory with mode 0700, and returns its path.
func writeFile(t *testing.T, env testenv.Env, data string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(env.ConfigHome, DirName, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// configPath returns the configuration path of env through Path.
func configPath(t *testing.T) string {
	t.Helper()
	path, err := Path(os.Getenv)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	return path
}

// checkError fails the test unless err wraps want and its text contains
// every string of contains.
func checkError(t *testing.T, err, want error, contains ...string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	for _, s := range contains {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not contain %q", err.Error(), s)
		}
	}
}
