package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Describe implements provider.Provider: the cached bot login (empty when
// setup or doctor has not resolved it), the installation ID, and what the
// credential is. It makes no network call.
func (p *Provider) Describe() []provider.Field {
	login := ""
	if p.cached != nil {
		login = p.cached.Login
	}
	return []provider.Field{
		{Label: "Identity", Value: login},
		{Label: "Installation", Value: strconv.FormatInt(p.installationID, 10)},
		{Label: "Credentials", Value: "GitHub App installation token"},
	}
}

// VerifyIdentity implements provider.Provider: it resolves the App's bot
// with Bot and compares it with the cached identity. When they differ, or
// the cache is empty, Refresh replaces the identity member of a github
// block and keeps every other member.
func (p *Provider) VerifyIdentity(ctx context.Context) (provider.IdentityCheck, error) {
	bot, err := p.Bot(ctx)
	if err != nil {
		return provider.IdentityCheck{}, err
	}
	check := provider.IdentityCheck{Bot: bot.Login}
	if p.cached != nil {
		check.Cached = p.cached.Login
	}
	if p.cached != nil && p.cached.Login == bot.Login && p.cached.UserID == bot.UserID {
		check.Match = true
		return check, nil
	}
	resolved := BotIdentity{Login: bot.Login, UserID: bot.UserID, ResolvedAt: p.now().UTC().Truncate(time.Second)}
	check.Refresh = func(settings json.RawMessage) (json.RawMessage, error) {
		return refreshIdentity(settings, resolved)
	}
	return check, nil
}

// refreshIdentity returns settings with identity as the cached bot.
func refreshIdentity(settings json.RawMessage, identity BotIdentity) (json.RawMessage, error) {
	decoded, err := DecodeProfileSettings(host, settings)
	if err != nil {
		return nil, fmt.Errorf("github: refresh the cached identity: %w", err)
	}
	decoded.Identity = &identity
	return json.Marshal(decoded)
}
