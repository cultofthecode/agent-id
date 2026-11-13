package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Request settings for the GitHub REST API.
const (
	maxResponseBytes = 1 << 20
	apiVersion       = "2022-11-28"
	userAgent        = "agent-id"
)

// errUnusableAnswer is wrapped with provider.ErrUnavailable when GitHub
// answers with something the adapter cannot use.
var errUnusableAnswer = errors.New("unusable answer from GitHub")

// call sends method to path under the base URL with the Authorization header
// authorization (none when empty) and decodes a response with status want
// into out. Errors name op and the HTTP status, never a header, a body, or a
// credential.
func (p *Provider) call(ctx context.Context, op, method, path, authorization string, want int, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("github: %s: %w: invalid request", op, provider.ErrUnavailable)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	request.Header.Set("User-Agent", userAgent)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return transportError(ctx, op, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != want {
		return fmt.Errorf("github: %s: HTTP %d: %w", op, response.StatusCode, classify(response.StatusCode, response.Header))
	}
	if err := decodeBody(response.Body, out); err != nil {
		return fmt.Errorf("github: %s: %w: %w", op, provider.ErrUnavailable, err)
	}
	return nil
}

// transportError reports a request that got no answer: the context's error
// when ctx ended, provider.ErrUnavailable otherwise. It keeps only the kind of
// failure, not the URL or the underlying message.
func transportError(ctx context.Context, op string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("github: %s: %w", op, ctxErr)
	}
	reason := "no answer"
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		reason = "timed out"
	}
	return fmt.Errorf("github: %s: %w: %s", op, provider.ErrUnavailable, reason)
}

// decodeBody decodes at most maxResponseBytes of body as JSON into out.
func decodeBody(body io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: incomplete body", errUnusableAnswer)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("%w: body larger than %d bytes", errUnusableAnswer, maxResponseBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: invalid JSON", errUnusableAnswer)
	}
	return nil
}

// classify returns the provider error for an unexpected HTTP status of the
// GitHub API, with its headers to tell rate limiting from a denial.
func classify(status int, header http.Header) error {
	switch {
	case status == http.StatusUnauthorized:
		return provider.ErrUnauthorized
	case status == http.StatusNotFound:
		return provider.ErrNotFound
	case status == http.StatusTooManyRequests:
		return provider.ErrRateLimited
	case status == http.StatusForbidden && (header.Get("X-RateLimit-Remaining") == "0" || header.Get("Retry-After") != ""):
		return provider.ErrRateLimited
	case status == http.StatusForbidden:
		return provider.ErrForbidden
	default:
		return provider.ErrUnavailable
	}
}

// checkBaseURL validates the API base URL: https, or http to a loopback IP
// address for a local fake; no user information, query, or fragment. It
// returns the URL without a trailing slash.
func checkBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: the API base URL must be an absolute URL without credentials, query, or fragment", ErrInvalidConfig)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("%w: the API base URL must use https", ErrInvalidConfig)
		}
	default:
		return "", fmt.Errorf("%w: the API base URL must use https", ErrInvalidConfig)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// noRedirects keeps the client on the API host: a redirect is returned as
// the response instead of followed.
func noRedirects(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}
