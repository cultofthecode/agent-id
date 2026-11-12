package githubtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

const installationID int64 = 77

func TestAppEndToEnd(t *testing.T) {
	app, client := newApp(t)
	app.AddInstallation(installationID, "cultofthecode/widgets", "cultofthecode/gadgets")
	jwt := appJWT(t, app, time.Now())

	var got struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	call(t, client, http.MethodGet, app.URL+"/app", "Bearer "+jwt, http.StatusOK, &got)
	if got.ID != AppID || got.Slug != AppSlug {
		t.Errorf("GET /app = %+v, want id %d and slug %s", got, AppID, AppSlug)
	}

	var minted struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	before := time.Now()
	call(t, client, http.MethodPost, tokenURL(app, installationID), "Bearer "+jwt, http.StatusCreated, &minted)
	if !testenv.IsFakeToken(minted.Token) {
		t.Errorf("minted token has no %s prefix", testenv.FakeTokenPrefix)
	}
	if lifetime := minted.ExpiresAt.Sub(before); lifetime < 59*time.Minute || lifetime > time.Hour {
		t.Errorf("token lifetime = %v, want one hour", lifetime)
	}
	if tokens := app.Tokens(); len(tokens) != 1 || tokens[0].Value != minted.Token || tokens[0].InstallationID != installationID {
		t.Errorf("Tokens() does not record the minted token for installation %d", installationID)
	}

	var repos struct {
		TotalCount   int `json:"total_count"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	call(t, client, http.MethodGet, app.URL+"/installation/repositories", "token "+minted.Token, http.StatusOK, &repos)
	if repos.TotalCount != 2 || repos.Repositories[0].FullName != "cultofthecode/widgets" || repos.Repositories[1].FullName != "cultofthecode/gadgets" {
		t.Errorf("GET /installation/repositories = %+v, want the two installed repositories", repos)
	}

	var bot struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"`
	}
	call(t, client, http.MethodGet, app.URL+"/users/"+BotLogin(), "", http.StatusOK, &bot)
	if bot.Login != "aidtest-agent[bot]" || bot.ID != BotUserID || bot.Type != "Bot" {
		t.Errorf("GET /users/%s = %+v, want the bot user", BotLogin(), bot)
	}
}

func TestAppRejectsInvalidJWTs(t *testing.T) {
	app, client := newApp(t)
	app.AddInstallation(installationID)
	otherKey, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	key := appKey(t, app)
	valid := map[string]any{"iss": AppID, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix()}
	with := func(name string, value any) map[string]any {
		claims := map[string]any{}
		for k, v := range valid {
			claims[k] = v
		}
		if value == nil {
			delete(claims, name)
		} else {
			claims[name] = value
		}
		return claims
	}
	tests := []struct {
		name          string
		authorization string
	}{
		{"no authorization", ""},
		{"not a JWT", "Bearer not-a-jwt"},
		{"installation token instead of a JWT", "Bearer " + testenv.FakeToken("1")},
		{"basic scheme", "Basic " + signJWT(t, key, "RS256", valid)},
		{"signed by another key", "Bearer " + signJWT(t, otherKey, "RS256", valid)},
		{"algorithm none", "Bearer " + unsignedJWT(t, valid)},
		{"algorithm HS256", "Bearer " + signJWT(t, key, "HS256", valid)},
		{"wrong issuer", "Bearer " + signJWT(t, key, "RS256", with("iss", AppID+1))},
		{"issuer as a wrong string", "Bearer " + signJWT(t, key, "RS256", with("iss", "aidtest-agent"))},
		{"no issuer", "Bearer " + signJWT(t, key, "RS256", with("iss", nil))},
		{"expired", "Bearer " + signJWT(t, key, "RS256", with("exp", now.Add(-time.Second).Unix()))},
		{"no expiry", "Bearer " + signJWT(t, key, "RS256", with("exp", nil))},
		{"issued in the future", "Bearer " + signJWT(t, key, "RS256", with("iat", now.Add(5*time.Minute).Unix()))},
		{"lives longer than ten minutes", "Bearer " + signJWT(t, key, "RS256", with("exp", now.Add(10*time.Minute).Unix()))},
		{"tampered payload", "Bearer " + tamper(t, signJWT(t, key, "RS256", valid))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call(t, client, http.MethodGet, app.URL+"/app", tt.authorization, http.StatusUnauthorized, nil)
			call(t, client, http.MethodPost, tokenURL(app, installationID), tt.authorization, http.StatusUnauthorized, nil)
		})
	}
	if len(app.Tokens()) != 0 {
		t.Errorf("the fake issued %d tokens for invalid JWTs", len(app.Tokens()))
	}
}

func TestAppAcceptsTheIssuerAsAString(t *testing.T) {
	app, client := newApp(t)
	now := time.Now()
	jwt := signJWT(t, appKey(t, app), "RS256", map[string]any{"iss": "424242", "iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix()})
	call(t, client, http.MethodGet, app.URL+"/app", "Bearer "+jwt, http.StatusOK, nil)
}

func TestTokenFailureModes(t *testing.T) {
	statuses := []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
	}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			app, client := newApp(t)
			app.AddInstallation(installationID)
			app.FailTokens(status)
			call(t, client, http.MethodPost, tokenURL(app, installationID), "Bearer "+appJWT(t, app, time.Now()), status, nil)
			if len(app.Tokens()) != 0 {
				t.Errorf("the fake issued a token while failing with %d", status)
			}
			app.FailTokens(0)
			call(t, client, http.MethodPost, tokenURL(app, installationID), "Bearer "+appJWT(t, app, time.Now()), http.StatusCreated, nil)
		})
	}
}

func TestTokenForAnUnknownInstallation(t *testing.T) {
	app, client := newApp(t)
	app.AddInstallation(installationID)
	call(t, client, http.MethodPost, tokenURL(app, installationID+1), "Bearer "+appJWT(t, app, time.Now()), http.StatusNotFound, nil)
	call(t, client, http.MethodPost, app.URL+"/app/installations/not-a-number/access_tokens", "Bearer "+appJWT(t, app, time.Now()), http.StatusNotFound, nil)
}

func TestTokenLifetime(t *testing.T) {
	tests := []struct {
		name       string
		lifetime   time.Duration
		wantStatus int
	}{
		{"ten minutes", 10 * time.Minute, http.StatusOK},
		{"already expired", -time.Minute, http.StatusUnauthorized},
		{"zero", 0, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, client := newApp(t)
			app.AddInstallation(installationID, "cultofthecode/widgets")
			app.SetTokenLifetime(tt.lifetime)
			var minted struct {
				Token     string    `json:"token"`
				ExpiresAt time.Time `json:"expires_at"`
			}
			before := time.Now().Truncate(time.Second)
			call(t, client, http.MethodPost, tokenURL(app, installationID), "Bearer "+appJWT(t, app, time.Now()), http.StatusCreated, &minted)
			if got := minted.ExpiresAt.Sub(before); got < tt.lifetime-time.Second || got > tt.lifetime+time.Second {
				t.Errorf("expires_at - now = %v, want %v", got, tt.lifetime)
			}
			call(t, client, http.MethodGet, app.URL+"/installation/repositories", "token "+minted.Token, tt.wantStatus, nil)
		})
	}
}

func TestInstallationRepositoriesRejectsTokensItDidNotIssue(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
	}{
		{"no authorization", ""},
		{"fake token never issued", "token " + testenv.FakeToken("never-issued")},
		{"App JWT", "Bearer header.payload.signature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, client := newApp(t)
			app.AddInstallation(installationID)
			call(t, client, http.MethodGet, app.URL+"/installation/repositories", tt.authorization, http.StatusUnauthorized, nil)
		})
	}
}

func TestAppReportsARealLookingToken(t *testing.T) {
	testenv.Isolate(t)
	rec := testenv.NewRecorder(t)
	fake := testenv.NewFakeGitHub(t)
	app := NewApp(rec, fake)
	app.AddInstallation(installationID)
	client := testenv.NewHTTPClient(t)
	token := "ghs_" + "realLookingToken"
	call(t, client, http.MethodGet, app.URL+"/installation/repositories", "token "+token, http.StatusUnauthorized, nil)
	call(t, client, http.MethodGet, app.URL+"/app", "Bearer "+token, http.StatusUnauthorized, nil)
	if !rec.Reported("the fake GitHub App received a real-looking GitHub token") || len(rec.Errors()) != 2 {
		t.Errorf("reported %q, want two reports of the real-looking token", rec.Errors())
	}
	for _, e := range rec.Errors() {
		if strings.Contains(e, token) {
			t.Errorf("the report %q contains the token", e)
		}
	}
}

func TestUnknownUserIsNotFound(t *testing.T) {
	app, client := newApp(t)
	for _, login := range []string{AppSlug, "octocat", "other-app[bot]"} {
		call(t, client, http.MethodGet, app.URL+"/users/"+login, "", http.StatusNotFound, nil)
	}
}

func TestPrivateKeyPEMIsTheAppKey(t *testing.T) {
	app, _ := newApp(t)
	block, _ := pem.Decode(app.PrivateKeyPEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatalf("PrivateKeyPEM is not a PKCS#1 PEM block")
	}
	other, _ := newApp(t)
	if string(other.PrivateKeyPEM) == string(app.PrivateKeyPEM) {
		t.Errorf("two Apps share a private key, want a new key per App")
	}
}

func TestVerifyJWTErrors(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	claims := func(iat, exp time.Time) map[string]any {
		return map[string]any{"iss": AppID, "iat": iat.Unix(), "exp": exp.Unix()}
	}
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"valid", signJWT(t, key, "RS256", claims(now.Add(-time.Minute), now.Add(9*time.Minute))), nil},
		{"two segments", "a.b", errJWTMalformed},
		{"bad base64", "!!!.e30.e30", errJWTMalformed},
		{"none", unsignedJWT(t, claims(now, now.Add(time.Minute))), errJWTAlgorithm},
		{"expires now", signJWT(t, key, "RS256", claims(now.Add(-time.Minute), now)), errJWTExpired},
		{"iat within the clock skew", signJWT(t, key, "RS256", claims(now.Add(time.Minute), now.Add(2*time.Minute))), nil},
		{"iat beyond the clock skew", signJWT(t, key, "RS256", claims(now.Add(time.Minute+time.Second), now.Add(2*time.Minute))), errJWTIssuedAt},
		{"exactly ten minutes", signJWT(t, key, "RS256", claims(now, now.Add(10*time.Minute))), nil},
		{"wrong issuer", signJWT(t, key, "RS256", map[string]any{"iss": 1, "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}), errJWTIssuer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := verifyJWT(tt.token, &key.PublicKey, AppID, now); !errors.Is(err, tt.want) {
				t.Errorf("verifyJWT = %v, want %v", err, tt.want)
			}
		})
	}
}

// newApp isolates the test and returns a fake App and an HTTP client that
// reaches only the fakes.
func newApp(t *testing.T) (*App, *http.Client) {
	t.Helper()
	testenv.Isolate(t)
	return NewApp(t, testenv.NewFakeGitHub(t)), testenv.NewHTTPClient(t)
}

// tokenURL returns the access token endpoint of installation id.
func tokenURL(app *App, id int64) string {
	return app.URL + "/app/installations/" + strconv.FormatInt(id, 10) + "/access_tokens"
}

// call sends a request with the Authorization header authorization (none when
// empty), checks the status, and decodes the JSON body into out when out is
// not nil.
func call(t *testing.T, client *http.Client, method, url, authorization string, wantStatus int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s = %d %s, want %d", method, url, resp.StatusCode, body, wantStatus)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, url, body, err)
		}
	}
}

// appKey parses the private key of app from its PEM.
func appKey(t *testing.T, app *App) *rsa.PrivateKey {
	t.Helper()
	block, _ := pem.Decode(app.PrivateKeyPEM)
	if block == nil {
		t.Fatal("PrivateKeyPEM holds no PEM block")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// appJWT returns a valid App JWT for app, issued a minute before now.
func appJWT(t *testing.T, app *App, now time.Time) string {
	t.Helper()
	return signJWT(t, appKey(t, app), "RS256", map[string]any{
		"iss": AppID,
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
	})
}

// signJWT returns a JWT with claims whose header names algorithm and whose
// signature is RS256 by key, whatever algorithm says.
func signJWT(t *testing.T, key *rsa.PrivateKey, algorithm string, claims map[string]any) string {
	t.Helper()
	signingInput := segment(t, map[string]any{"alg": algorithm, "typ": "JWT"}) + "." + segment(t, claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// unsignedJWT returns a JWT with claims, algorithm none, and no signature.
func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	return segment(t, map[string]any{"alg": "none", "typ": "JWT"}) + "." + segment(t, claims) + "."
}

// tamper replaces the payload of jwt with one for a longer lifetime and keeps
// the original signature.
func tamper(t *testing.T, jwt string) string {
	t.Helper()
	parts := strings.Split(jwt, ".")
	now := time.Now()
	parts[1] = segment(t, map[string]any{"iss": AppID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()})
	return strings.Join(parts, ".")
}

// segment returns v as a base64url JSON segment of a JWT.
func segment(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}
