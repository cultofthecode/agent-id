// Package githubtest is the fake GitHub of the test harness, on the GitHub
// side of the provider boundary (ADR #35). It serves the App,
// installation-token, installation-repository, and bot-user endpoints on a
// testenv.FakeAPI for the tests of the GitHub adapter, with a private key
// generated per test, controllable token lifetimes, and controllable
// failures. App.Provider puts the same fake behind provider.Provider, so the
// provider contract (providertest.RunContract) runs against it. It never talks
// to GitHub and issues only fake tokens (testenv.FakeToken).
//
// Only tests of internal/provider/github import this package; core tests use
// providertest.Fake (depguard).
package githubtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Identity of the fake App: test data, never a real App or account.
const (
	AppID     int64 = 424242
	AppSlug         = "aidtest-agent"
	AppName         = "aidtest agent"
	AppOwner        = "aidtest-owner"
	BotUserID int64 = 900001
)

// keyBits is the size of the generated App key; defaultTTL is the lifetime of
// an installation token, as on GitHub.
const (
	keyBits    = 2048
	defaultTTL = time.Hour
)

// App is a fake GitHub App registered on a testenv.FakeAPI.
type App struct {
	// PrivateKeyPEM is the PKCS#1 PEM private key of the App, generated for
	// the test, in the format GitHub issues.
	PrivateKeyPEM []byte
	// URL is the base URL of the fake API.
	URL string

	t   testing.TB
	key *rsa.PrivateKey

	mu            sync.Mutex
	installations map[int64][]string
	tokenLifetime time.Duration
	tokenFailure  int
	tokens        []Token
}

// Token is an installation token that the fake issued.
type Token struct {
	Value          string
	InstallationID int64
	ExpiresAt      time.Time
}

// NewApp registers the App endpoints on fake and returns the App. It
// generates a new private key for the test. Tokens live one hour unless
// SetTokenLifetime says otherwise.
func NewApp(t testing.TB, fake *testenv.FakeAPI) *App {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		t.Fatalf("githubtest: generate the App key: %v", err)
	}
	app := &App{
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
		URL:           fake.URL,
		t:             t,
		key:           key,
		installations: make(map[int64][]string),
		tokenLifetime: defaultTTL,
	}
	fake.Handle("GET /app", app.withJWT(app.getApp))
	fake.Handle("POST /app/installations/{id}/access_tokens", app.withJWT(app.createToken))
	fake.Handle("GET /users/{login}", app.getUser)
	fake.Handle("GET /installation/repositories", app.withToken(app.listRepositories))
	return app
}

// BotLogin returns the login of the App's bot user, "<slug>[bot]".
func BotLogin() string {
	return AppSlug + "[bot]"
}

// AddInstallation installs the App with id on repositories, each written
// "owner/name".
func (a *App) AddInstallation(id int64, repositories ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installations[id] = slices.Clone(repositories)
}

// SetTokenLifetime sets the lifetime of the tokens issued from now on. A
// lifetime of zero or less issues tokens that have already expired.
func (a *App) SetTokenLifetime(lifetime time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenLifetime = lifetime
}

// FailTokens makes every token request answer status, for example 401, 403,
// 404, or 503, until it is called with 0.
func (a *App) FailTokens(status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenFailure = status
}

// Tokens returns the tokens the fake issued, in order.
func (a *App) Tokens() []Token {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.tokens)
}

// withJWT passes requests with a valid App JWT to next and answers 401 to the
// others.
func (a *App) withJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credential, ok := a.credential(r, "Bearer")
		if !ok {
			writeError(w, http.StatusUnauthorized, errJWTMalformed.Error())
			return
		}
		if err := verifyJWT(credential, &a.key.PublicKey, AppID, time.Now()); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next(w, r)
	}
}

// withToken passes requests with an unexpired installation token that the
// fake issued to next and answers 401 to the others.
func (a *App) withToken(next func(http.ResponseWriter, *http.Request, Token)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credential, ok := a.credential(r, "token", "Bearer")
		if !ok {
			writeError(w, http.StatusUnauthorized, "Requires authentication")
			return
		}
		token, ok := a.issued(credential)
		if !ok || !time.Now().Before(token.ExpiresAt) {
			writeError(w, http.StatusUnauthorized, "Bad credentials")
			return
		}
		next(w, r, token)
	}
}

// credential returns the credential of the Authorization header of r when it
// uses one of schemes. It fails the test when the credential looks like a
// real GitHub token.
func (a *App) credential(r *http.Request, schemes ...string) (string, bool) {
	scheme, credential, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || credential == "" {
		return "", false
	}
	if testenv.LooksLikeRealToken(credential) {
		testenv.CheckFakeToken(a.t, "the fake GitHub App", credential)
		return "", false
	}
	for _, want := range schemes {
		if strings.EqualFold(scheme, want) {
			return credential, true
		}
	}
	return "", false
}

// issued returns the token with value, if the fake issued it.
func (a *App) issued(value string) (Token, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, token := range a.tokens {
		if token.Value == value {
			return token, true
		}
	}
	return Token{}, false
}

// getApp answers GET /app.
func (a *App) getApp(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       AppID,
		"slug":     AppSlug,
		"name":     AppName,
		"owner":    map[string]any{"login": AppOwner, "type": "Organization"},
		"html_url": "https://github.com/apps/" + AppSlug,
	})
}

// createToken answers POST /app/installations/{id}/access_tokens.
func (a *App) createToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	token, status := a.mint(id, time.Now())
	if status != http.StatusCreated {
		writeError(w, status, http.StatusText(status))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":                token.Value,
		"expires_at":           token.ExpiresAt.UTC().Format(time.RFC3339),
		"repository_selection": "selected",
	})
}

// mint issues a token for installation id, or returns the status of the
// configured failure or of an unknown installation.
func (a *App) mint(id int64, now time.Time) (Token, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tokenFailure != 0 {
		return Token{}, a.tokenFailure
	}
	if _, ok := a.installations[id]; !ok {
		return Token{}, http.StatusNotFound
	}
	token := Token{
		Value:          testenv.FakeToken(fmt.Sprintf("%d_%d", id, len(a.tokens)+1)),
		InstallationID: id,
		ExpiresAt:      now.Add(a.tokenLifetime).Truncate(time.Second),
	}
	a.tokens = append(a.tokens, token)
	return token, http.StatusCreated
}

// getUser answers GET /users/{login} for the App's bot user.
func (a *App) getUser(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("login") != BotLogin() {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"login": BotLogin(),
		"id":    BotUserID,
		"type":  "Bot",
	})
}

// listRepositories answers GET /installation/repositories with the
// repositories of the token's installation.
func (a *App) listRepositories(w http.ResponseWriter, _ *http.Request, token Token) {
	a.mu.Lock()
	names := slices.Clone(a.installations[token.InstallationID])
	a.mu.Unlock()
	repositories := make([]map[string]any, 0, len(names))
	for i, fullName := range names {
		owner, name, _ := strings.Cut(fullName, "/")
		repositories = append(repositories, map[string]any{
			"id":        i + 1,
			"name":      name,
			"full_name": fullName,
			"owner":     map[string]any{"login": owner},
			"private":   true,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_count":          len(repositories),
		"repository_selection": "selected",
		"repositories":         repositories,
	})
}

// writeError writes a GitHub error body with message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"message": message, "status": strconv.Itoa(status)})
}

// writeJSON writes body as JSON with status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
