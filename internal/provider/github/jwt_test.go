package github

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestSignJWT(t *testing.T) {
	testenv.Isolate(t)
	app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	key, err := parsePrivateKey(app.PrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	jwt, err := signJWT(key, 424242, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}
	if header := decode(t, parts[0]); header != `{"alg":"RS256","typ":"JWT"}` {
		t.Errorf("header = %s", header)
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal([]byte(decode(t, parts[1])), &claims); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"iat": "1799999940", "exp": "1800000540", "iss": "424242"}
	for name, value := range want {
		if string(claims[name]) != value {
			t.Errorf("claim %s = %s, want %s", name, claims[name], value)
		}
	}
	if len(claims) != len(want) {
		t.Errorf("claims = %v, want only iat, exp, iss", claims)
	}
	if lifetime := jwtLifetime + jwtBackdate; lifetime > 10*time.Minute {
		t.Errorf("exp - iat = %v, above GitHub's ten-minute limit", lifetime)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

func TestTheFakeAcceptsTheJWTWithinTheClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{-30 * time.Second, 0, 50 * time.Second} {
		app := newFakeApp(t)
		p, err := newProvider(Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM},
			api{baseURL: app.URL, client: testenv.NewHTTPClient(t), now: func() time.Time { return time.Now().Add(skew) }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Token(context.Background()); err != nil {
			t.Errorf("clock off by %v: Token: %v", skew, err)
		}
	}
}

func TestTheFakeRejectsAJWTOfAnotherApp(t *testing.T) {
	app := newFakeApp(t)
	other := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	tests := []struct {
		name string
		cfg  Config
	}{
		{"key of another App", Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: other.PrivateKeyPEM}},
		{"another App ID", Config{AppID: githubtest.AppID + 1, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustProvider(t, tt.cfg, app.URL)
			token, err := p.Token(context.Background())
			if !errors.Is(err, provider.ErrUnauthorized) || token != (provider.Token{}) {
				t.Errorf("Token: %v, want ErrUnauthorized and the zero Token", err)
			}
			if _, err := p.Bot(context.Background()); !errors.Is(err, provider.ErrUnauthorized) {
				t.Errorf("Bot: %v, want ErrUnauthorized", err)
			}
		})
	}
	if len(app.Tokens()) != 0 {
		t.Error("the fake issued a token for a foreign JWT")
	}
}

func TestAppRequestsCarryTheJWTAndGitHubHeaders(t *testing.T) {
	fake, p := newHostileAPI(t)
	var got http.Header
	fake.Handle("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		answer(http.StatusCreated, tokenBody(testenv.FakeToken("headers")))(w, r)
	})
	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Get("Authorization"), "Bearer eyJ") {
		t.Error("the token request has no Bearer JWT")
	}
	if got.Get("Accept") != "application/vnd.github+json" || got.Get("X-GitHub-Api-Version") != "2022-11-28" || got.Get("User-Agent") != "agent-id" {
		t.Errorf("headers = %v, want the GitHub REST headers", got)
	}
}

// decode decodes one base64url segment of a JWT.
func decode(t *testing.T, segment string) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
