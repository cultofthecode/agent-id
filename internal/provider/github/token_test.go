package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestTokenFromTheFakeApp(t *testing.T) {
	app := newFakeApp(t)
	p := newAdapter(t, app)
	token, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	issued := app.Tokens()
	if len(issued) != 1 || issued[0].Value != token.Value || issued[0].InstallationID != installationID {
		t.Fatalf("Token() = %v, want the token the fake issued for installation %d", token, installationID)
	}
	if !token.ExpiresAt.Equal(issued[0].ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", token.ExpiresAt, issued[0].ExpiresAt)
	}
}

func TestTokenFailuresMapToProviderErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, provider.ErrUnauthorized},
		{http.StatusForbidden, provider.ErrForbidden},
		{http.StatusNotFound, provider.ErrNotFound},
		{http.StatusTooManyRequests, provider.ErrRateLimited},
		{http.StatusInternalServerError, provider.ErrUnavailable},
		{http.StatusServiceUnavailable, provider.ErrUnavailable},
		{http.StatusTeapot, provider.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			app := newFakeApp(t)
			p := newAdapter(t, app)
			earlier, err := p.Token(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			app.FailTokens(tt.status)
			token, err := p.Token(context.Background())
			if !errors.Is(err, tt.want) || token != (provider.Token{}) {
				t.Fatalf("Token() with HTTP %d: %v, %v; want %v and the zero Token", tt.status, token, err, tt.want)
			}
			checkNoSecret(t, err.Error(), earlier.Value)
		})
	}
	t.Run("unknown installation", func(t *testing.T) {
		app := newFakeApp(t)
		p := mustProvider(t, Config{AppID: 424242, InstallationID: installationID + 1, PrivateKeyPEM: app.PrivateKeyPEM}, app.URL)
		if _, err := p.Token(context.Background()); !errors.Is(err, provider.ErrNotFound) {
			t.Errorf("Token() for an unknown installation: %v, want ErrNotFound", err)
		}
	})
}

func TestTokenRejectsUnusableAnswers(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{"no token", map[string]any{"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}},
		{"empty token", tokenBody("")},
		{"newline in the token", tokenBody(testenv.FakeToken("x\nusername=evil"))},
		{"space in the token", tokenBody(testenv.FakeToken("x y"))},
		{"non-ASCII token", tokenBody(testenv.FakeToken("zażółć"))},
		{"not an installation token", tokenBody("gho_aidtest_user")},
		{"too long", tokenBody(testenv.FakeToken(strings.Repeat("a", maxTokenLength)))},
		{"no expiry", map[string]any{"token": testenv.FakeToken("x")}},
		{"invalid expiry", map[string]any{"token": testenv.FakeToken("x"), "expires_at": "tomorrow"}},
		{"not an object", []string{"token"}},
		{"too large", map[string]any{"token": testenv.FakeToken("x"), "padding": strings.Repeat("a", maxResponseBytes)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, p := newHostileAPI(t)
			fake.Handle("POST /app/installations/{id}/access_tokens", answer(http.StatusCreated, tt.body))
			token, err := p.Token(context.Background())
			if !errors.Is(err, provider.ErrUnavailable) || token != (provider.Token{}) {
				t.Fatalf("Token() = %v, %v; want ErrUnavailable and the zero Token", token, err)
			}
			checkNoSecret(t, err.Error())
			if strings.Contains(err.Error(), "tomorrow") || strings.Contains(err.Error(), "evil") {
				t.Errorf("error %q repeats the answer", err)
			}
		})
	}
}

func TestTokenReportsAnExpiredToken(t *testing.T) {
	app := newFakeApp(t)
	app.SetTokenLifetime(-time.Minute)
	token, err := newAdapter(t, app).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !token.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt = %v, want the past expiry the host reported", token.ExpiresAt)
	}
}

func TestTokenDoesNotFollowRedirects(t *testing.T) {
	fake, p := newHostileAPI(t)
	elsewhere := testenv.NewFakeAPI(t)
	var reached atomic.Int32
	elsewhere.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		answer(http.StatusCreated, tokenBody(testenv.FakeToken("stolen")))(w, r)
	})
	fake.Handle("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/collect", http.StatusTemporaryRedirect)
	})
	token, err := p.Token(context.Background())
	if !errors.Is(err, provider.ErrUnavailable) || token != (provider.Token{}) {
		t.Errorf("Token() after a redirect: %v, %v; want ErrUnavailable and the zero Token", token, err)
	}
	if reached.Load() != 0 {
		t.Error("the adapter followed the redirect and sent its JWT to another listener")
	}
}

func TestTokenStopsWhenTheContextEnds(t *testing.T) {
	fake, p := newHostileAPI(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake.Handle("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := p.Token(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Token() past the deadline: %v, want context.DeadlineExceeded", err)
	}
}

func TestTokenStopsWhenTheContextEndsDuringTheBody(t *testing.T) {
	fake, p := newHostileAPI(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake.Handle("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":`))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := p.Token(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Token() past the deadline during the body: %v, want context.DeadlineExceeded", err)
	}
}

func TestTokenWithoutAnAnswer(t *testing.T) {
	app := newFakeApp(t)
	listener := testenv.Listen(t)
	url := "http://" + listener.Addr().String()
	_ = listener.Close()
	p := mustProvider(t, Config{AppID: 424242, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}, url)
	_, err := p.Token(context.Background())
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("Token() without a server: %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error %q repeats the transport error", err)
	}
}
