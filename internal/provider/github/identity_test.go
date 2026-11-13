package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
)

func TestCommitIdentity(t *testing.T) {
	identity, err := CommitIdentity("claude-dev[bot]", 12345678)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Name != "claude-dev[bot]" || identity.Email != "12345678+claude-dev[bot]@users.noreply.github.com" {
		t.Errorf("CommitIdentity = %+v, want claude-dev[bot] and its noreply address", identity)
	}
	hostile := []struct {
		login string
		id    int64
	}{
		{"claude-dev", 1},
		{"[bot]", 1},
		{"Claude-Dev[bot]", 1},
		{"-claude[bot]", 1},
		{"claude dev[bot]", 1},
		{"claude\n[bot]", 1},
		{"claude@evil.test[bot]", 1},
		{strings.Repeat("a", 64) + "[bot]", 1},
		{"claude-dev[bot]", 0},
		{"claude-dev[bot]", -5},
	}
	for _, tt := range hostile {
		if got, err := CommitIdentity(tt.login, tt.id); err == nil || got != (provider.Identity{}) {
			t.Errorf("CommitIdentity(%q, %d) = %+v, %v; want an error and no identity", tt.login, tt.id, got, err)
		}
	}
}

func TestIdentityOfTheFakeApp(t *testing.T) {
	app := newFakeApp(t)
	p := newAdapter(t, app)
	bot, err := p.Bot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bot.Login != githubtest.BotLogin() || bot.UserID != githubtest.BotUserID {
		t.Errorf("Bot() = %+v, want %s with id %d", bot, githubtest.BotLogin(), githubtest.BotUserID)
	}
	identity, err := p.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Name != githubtest.BotLogin() || identity.Email != githubtest.BotEmail() {
		t.Errorf("Identity() = %+v, want %s <%s>", identity, githubtest.BotLogin(), githubtest.BotEmail())
	}
}

func TestBotRejectsHostileAnswers(t *testing.T) {
	user := func(login, kind string, id int64) map[string]any {
		return map[string]any{"login": login, "type": kind, "id": id}
	}
	tests := []struct {
		name string
		slug string
		user map[string]any
	}{
		{"another login", "claude-dev", user("someone-else", "Bot", 1)},
		{"a user, not a bot", "claude-dev", user("claude-dev[bot]", "User", 1)},
		{"id zero", "claude-dev", user("claude-dev[bot]", "Bot", 0)},
		{"path in the slug", "../users/x", user("claude-dev[bot]", "Bot", 1)},
		{"uppercase slug", "Claude", user("Claude[bot]", "Bot", 1)},
		{"slug too long", strings.Repeat("a", 64), user(strings.Repeat("a", 64)+"[bot]", "Bot", 1)},
		{"empty slug", "", user("[bot]", "Bot", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, p := newHostileAPI(t)
			fake.Handle("GET /app", answer(http.StatusOK, map[string]any{"slug": tt.slug}))
			fake.Handle("GET /users/{login}", answer(http.StatusOK, tt.user))
			if bot, err := p.Bot(context.Background()); !errors.Is(err, provider.ErrUnavailable) || bot != (BotUser{}) {
				t.Errorf("Bot() = %+v, %v; want ErrUnavailable", bot, err)
			}
			if identity, err := p.Identity(context.Background()); !errors.Is(err, provider.ErrUnavailable) || identity != (provider.Identity{}) {
				t.Errorf("Identity() = %+v, %v; want ErrUnavailable", identity, err)
			}
		})
	}
}

func TestBotSendsNoCredentialToTheUsersEndpoint(t *testing.T) {
	fake, p := newHostileAPI(t)
	var authorization []string
	fake.Handle("GET /app", answer(http.StatusOK, map[string]any{"slug": "claude-dev"}))
	fake.Handle("GET /users/{login}", func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		if r.PathValue("login") != "claude-dev[bot]" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer(http.StatusOK, map[string]any{"login": "claude-dev[bot]", "type": "Bot", "id": 12345678})(w, r)
	})
	identity, err := p.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "12345678+claude-dev[bot]@users.noreply.github.com" {
		t.Errorf("Identity() = %+v", identity)
	}
	if len(authorization) != 1 || authorization[0] != "" {
		t.Errorf("GET /users carried Authorization %q, want none", authorization)
	}
}

func TestBotFailures(t *testing.T) {
	tests := []struct {
		name   string
		app    int
		users  int
		header http.Header
		want   error
	}{
		{"App JWT rejected", http.StatusUnauthorized, http.StatusOK, nil, provider.ErrUnauthorized},
		{"bot user missing", http.StatusOK, http.StatusNotFound, nil, provider.ErrNotFound},
		{"rate limited", http.StatusOK, http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}}, provider.ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, p := newHostileAPI(t)
			fake.Handle("GET /app", answer(tt.app, map[string]any{"slug": "claude-dev"}))
			fake.Handle("GET /users/{login}", func(w http.ResponseWriter, r *http.Request) {
				for name, values := range tt.header {
					w.Header()[name] = values
				}
				answer(tt.users, map[string]any{"login": "claude-dev[bot]", "type": "Bot", "id": 1})(w, r)
			})
			if _, err := p.Identity(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("Identity(): %v, want %v", err, tt.want)
			}
		})
	}
}
