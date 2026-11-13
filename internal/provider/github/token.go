package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Shape of an installation token: the installation-token prefix and a bound
// on the length.
const (
	installationTokenPrefix = "ghs_"
	maxTokenLength          = 4096
)

// tokenAnswer is the answer of POST /app/installations/{id}/access_tokens.
type tokenAnswer struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// Token implements provider.Provider: it mints a new installation token with
// an App JWT and returns it with the expiry GitHub reported, expired or not.
// A failed Token returns the zero Token.
func (p *Provider) Token(ctx context.Context) (provider.Token, error) {
	const op = "create an installation token"
	authorization, err := p.appAuthorization(op)
	if err != nil {
		return provider.Token{}, err
	}
	var answer tokenAnswer
	path := "/app/installations/" + strconv.FormatInt(p.installationID, 10) + "/access_tokens"
	if err := p.call(ctx, op, http.MethodPost, path, authorization, http.StatusCreated, &answer); err != nil {
		return provider.Token{}, err
	}
	token, err := parseToken(answer)
	if err != nil {
		return provider.Token{}, fmt.Errorf("github: %s: %w: %w", op, provider.ErrUnavailable, err)
	}
	return token, nil
}

// parseToken checks a token answer: an installation token (prefix ghs_) of
// at most maxTokenLength visible ASCII characters, so that it cannot break a
// credential helper answer or an environment entry, and an RFC 3339 expiry.
func parseToken(answer tokenAnswer) (provider.Token, error) {
	if !validToken(answer.Token) || !strings.HasPrefix(answer.Token, installationTokenPrefix) {
		return provider.Token{}, fmt.Errorf("%w: not an installation token of at most %d visible ASCII characters", errUnusableAnswer, maxTokenLength)
	}
	expiresAt, err := time.Parse(time.RFC3339, answer.ExpiresAt)
	if err != nil {
		return provider.Token{}, fmt.Errorf("%w: expires_at is not an RFC 3339 time", errUnusableAnswer)
	}
	return provider.Token{Value: answer.Token, ExpiresAt: expiresAt}, nil
}

// validToken reports whether token has 1 to maxTokenLength characters, each
// in visible ASCII (0x21–0x7e).
func validToken(token string) bool {
	if token == "" || len(token) > maxTokenLength {
		return false
	}
	for i := range len(token) {
		if token[i] < 0x21 || token[i] > 0x7e {
			return false
		}
	}
	return true
}
