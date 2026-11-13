package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// slugPattern is the shape of a GitHub App slug that the adapter accepts.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// BotUser is the GitHub user of a GitHub App: login "<slug>[bot]" and its
// numeric id.
type BotUser struct {
	Login  string
	UserID int64
}

// appAnswer is the part of GET /app that the adapter reads.
type appAnswer struct {
	Slug string `json:"slug"`
}

// userAnswer is the part of GET /users/{login} that the adapter reads.
type userAnswer struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

// Bot resolves the App's bot user: the App slug from GET /app with an App
// JWT, then the bot's id from GET /users/{slug}[bot] without credentials.
func (p *Provider) Bot(ctx context.Context) (BotUser, error) {
	const op = "resolve the bot user"
	authorization, err := p.appAuthorization(op)
	if err != nil {
		return BotUser{}, err
	}
	var app appAnswer
	if err := p.call(ctx, op, http.MethodGet, "/app", authorization, http.StatusOK, &app); err != nil {
		return BotUser{}, err
	}
	if !slugPattern.MatchString(app.Slug) {
		return BotUser{}, fmt.Errorf("github: %s: %w: %w: the App slug is not a GitHub App slug", op, provider.ErrUnavailable, errUnusableAnswer)
	}
	login := botLogin(app.Slug)
	var user userAnswer
	if err := p.call(ctx, op, http.MethodGet, "/users/"+url.PathEscape(login), "", http.StatusOK, &user); err != nil {
		return BotUser{}, err
	}
	if user.Login != login || user.Type != "Bot" || user.ID <= 0 {
		return BotUser{}, fmt.Errorf("github: %s: %w: %w: the user is not the App's bot", op, provider.ErrUnavailable, errUnusableAnswer)
	}
	return BotUser{Login: login, UserID: user.ID}, nil
}

// Identity implements provider.Provider with the commit identity of the
// App's bot user.
func (p *Provider) Identity(ctx context.Context) (provider.Identity, error) {
	bot, err := p.Bot(ctx)
	if err != nil {
		return provider.Identity{}, err
	}
	return CommitIdentity(bot.Login, bot.UserID)
}

// CommitIdentity returns the commit author and committer of a GitHub App bot:
// name "<slug>[bot]" and email "<id>+<slug>[bot]@users.noreply.github.com".
// It fails for a login that is not "<slug>[bot]" or an id that is not
// positive.
func CommitIdentity(login string, userID int64) (provider.Identity, error) {
	slug, ok := botSlug(login)
	if !ok || userID <= 0 {
		return provider.Identity{}, fmt.Errorf("github: %w: not a GitHub App bot user", ErrInvalidConfig)
	}
	name := botLogin(slug)
	return provider.Identity{Name: name, Email: strconv.FormatInt(userID, 10) + "+" + name + "@users.noreply.github.com"}, nil
}

// botLogin returns the login of the bot user of the App with slug.
func botLogin(slug string) string {
	return slug + "[bot]"
}

// botSlug returns the App slug of a bot login "<slug>[bot]".
func botSlug(login string) (string, bool) {
	const suffix = "[bot]"
	if len(login) <= len(suffix) || login[len(login)-len(suffix):] != suffix {
		return "", false
	}
	slug := login[:len(login)-len(suffix)]
	return slug, slugPattern.MatchString(slug)
}
