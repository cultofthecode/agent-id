package github

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

// The fail-closed suite of #25 for the in-process chain: key file, adapter,
// token manager, fake GitHub. Every scenario must end in an error that names
// its cause, the zero Token, no token or key in the error, and no token in
// the cache; nothing in the chain reads a personal credential, so a decoy
// GH_TOKEN, GITHUB_TOKEN and hosts.yml stay untouched.

const personalDecoy = "ghp_aidtest_personal_decoy"

type chain struct {
	t       *testing.T
	app     *githubtest.App
	cache   string
	keyPath string
	hosts   string
	now     time.Time
	clock   func() time.Time
}

func newChain(t *testing.T) *chain {
	t.Helper()
	env := testenv.Isolate(t)
	t.Setenv("GH_TOKEN", personalDecoy)
	t.Setenv("GITHUB_TOKEN", personalDecoy)
	app := githubtest.NewApp(t, testenv.NewFakeAPI(t))
	app.AddInstallation(installationID)
	c := &chain{t: t, app: app, cache: filepath.Join(env.CacheHome, "agent-id", "tokens"), keyPath: filepath.Join(env.Home, "app.pem"), hosts: filepath.Join(env.GHConfigDir, "hosts.yml")}
	c.now = time.Now()
	c.clock = func() time.Time { return c.now }
	if err := os.WriteFile(c.keyPath, app.PrivateKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.hosts, []byte("github.com:\n    oauth_token: "+personalDecoy+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *chain) setKey(mode os.FileMode, content []byte) {
	c.t.Helper()
	if err := os.WriteFile(c.keyPath, content, mode); err != nil {
		c.t.Fatal(err)
	}
	if err := os.Chmod(c.keyPath, mode); err != nil {
		c.t.Fatal(err)
	}
}

func (c *chain) manager() *token.Manager {
	return &token.Manager{Dir: c.cache, Now: c.clock}
}

// token runs the whole chain once.
func (c *chain) token(baseURL string, appID, installation int64, adapterNow func() time.Time) (provider.Token, error) {
	pem, err := credential.File{Path: c.keyPath}.PrivateKeyPEM()
	if err != nil {
		return provider.Token{}, err
	}
	p, err := newProvider(Config{AppID: appID, InstallationID: installation, PrivateKeyPEM: pem}, api{baseURL: baseURL, client: testenv.NewHTTPClient(c.t), now: adapterNow})
	if err != nil {
		return provider.Token{}, err
	}
	return c.manager().Token(context.Background(), p)
}

func (c *chain) ok() (provider.Token, error) {
	return c.token(c.app.URL, githubtest.AppID, installationID, nil)
}

// assertDenied checks the common outcome of every scenario.
func (c *chain) assertDenied(tok provider.Token, err, want error) {
	c.t.Helper()
	if !errors.Is(err, want) {
		c.t.Fatalf("error %v, want %v", err, want)
	}
	if tok != (provider.Token{}) {
		c.t.Errorf("a token came back with the error")
	}
	checkNoSecret(c.t, err.Error(), personalDecoy)
	for _, issued := range c.app.Tokens() {
		checkNoSecret(c.t, err.Error(), issued.Value)
	}
	if strings.Contains(err.Error(), "BEGIN") {
		c.t.Errorf("the error holds key material")
	}
	c.assertCacheHoldsNoToken()
	if data, _ := os.ReadFile(c.hosts); !strings.Contains(string(data), personalDecoy) {
		c.t.Error("hosts.yml was changed")
	}
}

func (c *chain) assertCacheHoldsNoToken() {
	c.t.Helper()
	entries, _ := os.ReadDir(c.cache)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		c.t.Errorf("the cache holds %s after a refusal", e.Name())
	}
}

func TestFailClosedKeyFile(t *testing.T) {
	good := func(c *chain) []byte { return c.app.PrivateKeyPEM }
	tests := []struct {
		name  string
		setup func(c *chain)
		want  error
	}{
		{"missing key file", func(c *chain) { _ = os.Remove(c.keyPath) }, credential.ErrKeyNotFound},
		{"group-readable key", func(c *chain) { c.setKey(0o640, good(c)) }, credential.ErrInsecureKeyFile},
		{"world-readable key", func(c *chain) { c.setKey(0o644, good(c)) }, credential.ErrInsecureKeyFile},
		{"unreadable key", func(c *chain) { c.setKey(0o000, good(c)) }, credential.ErrKeyUnreadable},
		{"empty file", func(c *chain) { c.setKey(0o600, nil) }, credential.ErrInvalidKey},
		{"not PEM", func(c *chain) { c.setKey(0o600, []byte("not a key\n")) }, credential.ErrInvalidKey},
		{"truncated PEM", func(c *chain) { c.setKey(0o600, good(c)[:200]) }, credential.ErrInvalidKey},
		{"two keys", func(c *chain) { c.setKey(0o600, append(good(c), good(c)...)) }, credential.ErrInvalidKey},
		{"certificate instead of key", func(c *chain) {
			c.setKey(0o600, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"))
		}, credential.ErrInvalidKey},
		{"relative reference", func(c *chain) { c.keyPath = "app.pem" }, credential.ErrInvalidReference},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChain(t)
			tt.setup(c)
			tok, err := c.ok()
			c.assertDenied(tok, err, tt.want)
			if reqs := len(c.app.Tokens()); reqs != 0 {
				t.Errorf("GitHub issued %d tokens although the key was refused", reqs)
			}
		})
	}
}

func TestFailClosedSymlinkedKeyToBadTarget(t *testing.T) {
	c := newChain(t)
	target := filepath.Join(filepath.Dir(c.keyPath), "loose.pem")
	if err := os.WriteFile(target, c.app.PrivateKeyPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(c.keyPath), "link.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c.keyPath = link
	tok, err := c.ok()
	c.assertDenied(tok, err, credential.ErrInsecureKeyFile)
}

func TestFailClosedHostAnswers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"401 rejected", http.StatusUnauthorized, provider.ErrUnauthorized},
		{"403 denied", http.StatusForbidden, provider.ErrForbidden},
		{"404 installation gone", http.StatusNotFound, provider.ErrNotFound},
		{"429 rate limited", http.StatusTooManyRequests, provider.ErrRateLimited},
		{"500", http.StatusInternalServerError, provider.ErrUnavailable},
		{"503", http.StatusServiceUnavailable, provider.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChain(t)
			c.app.FailTokens(tt.status)
			tok, err := c.ok()
			c.assertDenied(tok, err, tt.want)
		})
	}
}

func TestFailClosedWrongIdentity(t *testing.T) {
	t.Run("another App's key", func(t *testing.T) {
		c := newChain(t)
		other := githubtest.NewApp(t, testenv.NewFakeAPI(t))
		c.setKey(0o600, other.PrivateKeyPEM)
		tok, err := c.ok()
		c.assertDenied(tok, err, provider.ErrUnauthorized)
	})
	t.Run("another App ID", func(t *testing.T) {
		c := newChain(t)
		tok, err := c.token(c.app.URL, githubtest.AppID+1, installationID, nil)
		c.assertDenied(tok, err, provider.ErrUnauthorized)
	})
	t.Run("deleted installation", func(t *testing.T) {
		c := newChain(t)
		tok, err := c.token(c.app.URL, githubtest.AppID, installationID+1, nil)
		c.assertDenied(tok, err, provider.ErrNotFound)
	})
}

func TestFailClosedNetworkDown(t *testing.T) {
	c := newChain(t)
	l := testenv.Listen(t)
	down := "http://" + l.Addr().String()
	_ = l.Close()
	tok, err := c.token(down, githubtest.AppID, installationID, nil)
	c.assertDenied(tok, err, provider.ErrUnavailable)
}

func TestFailClosedBadClock(t *testing.T) {
	for name, skew := range map[string]time.Duration{"clock an hour fast": time.Hour, "clock a day slow": -24 * time.Hour} {
		t.Run(name, func(t *testing.T) {
			c := newChain(t)
			tok, err := c.token(c.app.URL, githubtest.AppID, installationID, func() time.Time { return time.Now().Add(skew) })
			c.assertDenied(tok, err, provider.ErrUnauthorized)
		})
	}
	t.Run("host answers an already expired token", func(t *testing.T) {
		c := newChain(t)
		c.app.SetTokenLifetime(0)
		tok, err := c.ok()
		c.assertDenied(tok, err, token.ErrUnusableToken)
	})
	t.Run("token inside the refresh margin", func(t *testing.T) {
		c := newChain(t)
		c.app.SetTokenLifetime(time.Minute)
		tok, err := c.ok()
		c.assertDenied(tok, err, token.ErrUnusableToken)
	})
	t.Run("manager clock far ahead of the host", func(t *testing.T) {
		c := newChain(t)
		c.now = time.Now().Add(48 * time.Hour)
		tok, err := c.ok()
		c.assertDenied(tok, err, token.ErrUnusableToken)
	})
}

// TestFailClosedExpiredCachedToken proves that an expired entry is never
// served as a fallback when the refresh fails, whatever the failure is.
func TestFailClosedExpiredCachedToken(t *testing.T) {
	failures := []struct {
		name   string
		break_ func(c *chain)
		want   error
	}{
		{"mint 401", func(c *chain) { c.app.FailTokens(401) }, provider.ErrUnauthorized},
		{"mint 403", func(c *chain) { c.app.FailTokens(403) }, provider.ErrForbidden},
		{"mint 404", func(c *chain) { c.app.FailTokens(404) }, provider.ErrNotFound},
		{"mint 503", func(c *chain) { c.app.FailTokens(503) }, provider.ErrUnavailable},
		{"key removed", func(c *chain) { _ = os.Remove(c.keyPath) }, credential.ErrKeyNotFound},
		{"key made world-readable", func(c *chain) { c.setKey(0o644, c.app.PrivateKeyPEM) }, credential.ErrInsecureKeyFile},
		{"key replaced by garbage", func(c *chain) { c.setKey(0o600, []byte("garbage")) }, credential.ErrInvalidKey},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			c := newChain(t)
			first, err := c.ok()
			if err != nil {
				t.Fatal(err)
			}
			again, err := c.ok()
			if err != nil || again != first {
				t.Fatalf("a fresh entry was not served from the cache: %v", err)
			}
			c.now = c.now.Add(2 * time.Hour)
			tt.break_(c)
			tok, err := c.ok()
			if !errors.Is(err, tt.want) {
				t.Fatalf("error %v, want %v", err, tt.want)
			}
			if tok != (provider.Token{}) {
				t.Error("the expired token came back")
			}
			checkNoSecret(t, err.Error(), first.Value, personalDecoy)
		})
	}
	t.Run("inside the margin counts as expired", func(t *testing.T) {
		c := newChain(t)
		first, err := c.ok()
		if err != nil {
			t.Fatal(err)
		}
		c.now = first.ExpiresAt.Add(-time.Minute)
		c.app.FailTokens(503)
		if tok, err := c.ok(); !errors.Is(err, provider.ErrUnavailable) || tok != (provider.Token{}) {
			t.Errorf("got %v, %v; want ErrUnavailable and the zero Token", tok, err)
		}
	})
}

func TestFailClosedRecoversOnlyWithAValidMint(t *testing.T) {
	c := newChain(t)
	c.app.FailTokens(403)
	if _, err := c.ok(); !errors.Is(err, provider.ErrForbidden) {
		t.Fatal(err)
	}
	c.app.FailTokens(0)
	tok, err := c.ok()
	if err != nil || !strings.HasPrefix(tok.Value, testenv.FakeTokenPrefix) {
		t.Fatalf("after the host recovered: %v, %v", tok, err)
	}
}
