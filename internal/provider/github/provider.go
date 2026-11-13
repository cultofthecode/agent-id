// Package github is the GitHub adapter of the provider interface (ADR #35):
// it signs the GitHub App JWT with the standard library, mints installation
// tokens, resolves the App's bot user, and lists the repositories of an
// installation. It is the only package with GitHub-specific code; only the
// composition root (cmd/agent-id) constructs it.
//
// No error of this package contains a token, a JWT, or key material.
package github

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Fixed GitHub.com values. GitHub Enterprise Server is out of scope (brief
// §8).
const (
	providerName   = "github"
	host           = "github.com"
	defaultBaseURL = "https://api.github.com"
	gitUsername    = "x-access-token"
	defaultTimeout = 30 * time.Second
)

// Errors of New. They describe the problem and never contain key material.
var (
	ErrInvalidConfig = errors.New("github: invalid adapter configuration")
	ErrInvalidKey    = errors.New("github: invalid App private key")
)

// Config is what the adapter needs to act as one installation of a GitHub
// App.
type Config struct {
	// AppID and InstallationID identify the App and its installation.
	AppID          int64
	InstallationID int64
	// PrivateKeyPEM is the App's private key, PKCS#1 or PKCS#8 RSA of at
	// least 2048 bits, as loaded by the credential source. New keeps only
	// the parsed key. Neither fmt nor encoding/json prints it.
	PrivateKeyPEM []byte `json:"-"`
}

// Format implements fmt.Formatter: it prints the IDs and redacts the key.
func (c Config) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "github.Config{AppID: "+strconv.FormatInt(c.AppID, 10)+", InstallationID: "+strconv.FormatInt(c.InstallationID, 10)+", PrivateKeyPEM: [redacted]}")
}

// api is where and how the adapter reaches the GitHub API. Production uses
// the zero value: api.github.com, a client with a 30-second timeout, and
// time.Now. Only the package's tests set it, so that no caller can send the
// App JWT to another host.
type api struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

// Provider is the GitHub adapter for one installation of a GitHub App. It
// implements provider.Provider. Formatting it prints only its IDs.
type Provider struct {
	appID          int64
	installationID int64
	key            *rsa.PrivateKey
	baseURL        string
	client         *http.Client
	now            func() time.Time
}

// New returns the adapter for cfg on api.github.com after checking the IDs
// and the private key.
func New(cfg Config) (*Provider, error) {
	return newProvider(cfg, api{})
}

// newProvider returns the adapter for cfg on a.
func newProvider(cfg Config, a api) (*Provider, error) {
	if cfg.AppID <= 0 || cfg.InstallationID <= 0 {
		return nil, fmt.Errorf("%w: the App ID and the installation ID must be positive", ErrInvalidConfig)
	}
	baseURL := a.baseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL, err := checkBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	key, err := parsePrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &Provider{
		appID:          cfg.AppID,
		installationID: cfg.InstallationID,
		key:            key,
		baseURL:        baseURL,
		client:         withoutRedirects(a.client),
		now:            clock(a.now),
	}, nil
}

// withoutRedirects returns a copy of client, or a default client, that never
// follows a redirect.
func withoutRedirects(client *http.Client) *http.Client {
	c := http.Client{Timeout: defaultTimeout}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = noRedirects
	return &c
}

// clock returns now, or time.Now when now is nil.
func clock(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

// Format implements fmt.Formatter: it prints the App and installation IDs,
// never the key.
func (p Provider) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "github.Provider{AppID: "+strconv.FormatInt(p.appID, 10)+", InstallationID: "+strconv.FormatInt(p.installationID, 10)+"}")
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return providerName }

// Host implements provider.Provider.
func (p *Provider) Host() string { return host }

// TokenSubject implements provider.Provider: an installation token belongs
// to one installation of one App.
func (p *Provider) TokenSubject() string {
	return "app-" + strconv.FormatInt(p.appID, 10) + "-installation-" + strconv.FormatInt(p.installationID, 10)
}

// GitCredential implements provider.Provider: GitHub takes an installation
// token over HTTPS with the username x-access-token.
func (p *Provider) GitCredential(token provider.Token) provider.GitCredential {
	return provider.GitCredential{Protocol: "https", Host: host, Username: gitUsername, Password: token.Value}
}

// CLIEnvironment implements provider.Provider for gh: the token in GH_TOKEN
// and the host pinned with GH_HOST. The result holds a secret.
func (p *Provider) CLIEnvironment(token provider.Token) []string {
	return []string{"GH_TOKEN=" + token.Value, "GH_HOST=" + host}
}

// appAuthorization returns the Authorization header of an App request, with
// a new App JWT.
func (p *Provider) appAuthorization(op string) (string, error) {
	jwt, err := signJWT(p.key, p.appID, p.now())
	if err != nil {
		return "", fmt.Errorf("github: %s: %w", op, err)
	}
	return "Bearer " + jwt, nil
}
