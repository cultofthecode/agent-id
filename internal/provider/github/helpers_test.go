package github

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

const installationID int64 = 77

// newFakeApp isolates the test and starts the fake GitHub App with
// installation installationID on repositories.
func newFakeApp(t *testing.T, repositories ...string) *githubtest.App {
	t.Helper()
	testenv.Isolate(t)
	app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	app.AddInstallation(installationID, repositories...)
	return app
}

// newAdapter returns the adapter for the fake App, on the fake API.
func newAdapter(t *testing.T, app *githubtest.App) *Provider {
	t.Helper()
	return mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}, app.URL)
}

// mustProvider returns the adapter for cfg on the API at baseURL, through the
// test client that reaches only the fakes.
func mustProvider(t *testing.T, cfg Config, baseURL string) *Provider {
	t.Helper()
	p, err := newProvider(cfg, api{baseURL: baseURL, client: testenv.NewHTTPClient(t)})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	return p
}

// newHostileAPI isolates the test and starts a fake API whose routes the
// test registers, with an adapter for it that uses the key of a fake App.
func newHostileAPI(t *testing.T) (*testenv.FakeAPI, *Provider) {
	t.Helper()
	testenv.Isolate(t)
	keyAPI := testenv.NewFakeAPI(t)
	app := githubtest.NewApp(t, keyAPI)
	fake := testenv.NewFakeAPI(t)
	return fake, mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}, fake.URL)
}

// answer returns a handler that writes body as JSON with status.
func answer(status int, body any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

// tokenBody is a token answer with value and an expiry an hour from now.
func tokenBody(value string) map[string]any {
	return map[string]any{"token": value, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}

// checkNoSecret fails the test when text holds a token or a JWT.
func checkNoSecret(t *testing.T, text string, tokens ...string) {
	t.Helper()
	if strings.Contains(text, testenv.FakeTokenPrefix) || strings.Contains(text, "eyJ") {
		t.Errorf("%q holds a token or a JWT", text)
	}
	for _, token := range tokens {
		if token != "" && strings.Contains(text, token) {
			t.Errorf("%q holds a token", text)
		}
	}
}
