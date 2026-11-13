package github

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
)

func TestDescribeReadsTheCachedIdentityWithoutANetworkCall(t *testing.T) {
	app := newFakeApp(t)
	cached := &BotIdentity{Login: githubtest.BotLogin(), UserID: githubtest.BotUserID, ResolvedAt: time.Unix(1, 0).UTC()}
	p := mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM, Cached: cached}, "http://127.0.0.1:1")
	want := []provider.Field{
		{Label: "Identity", Value: githubtest.BotLogin()},
		{Label: "Installation", Value: "77"},
		{Label: "Credentials", Value: "GitHub App installation token"},
	}
	if got := p.Describe(); !slices.Equal(got, want) {
		t.Errorf("Describe() = %v, want %v", got, want)
	}
	uncached := mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM}, "http://127.0.0.1:1")
	if got := uncached.Describe()[0]; got.Value != "" {
		t.Errorf("Describe() without a cache: Identity = %q, want empty", got.Value)
	}
}

func TestVerifyIdentityMatchesAndRefreshes(t *testing.T) {
	app := newFakeApp(t)
	current := &BotIdentity{Login: githubtest.BotLogin(), UserID: githubtest.BotUserID, ResolvedAt: time.Unix(1, 0).UTC()}
	match := mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM, Cached: current}, app.URL)
	check, err := match.VerifyIdentity(context.Background())
	if err != nil || !check.Match || check.Refresh != nil || check.Bot != githubtest.BotLogin() {
		t.Fatalf("VerifyIdentity() with the current cache = %+v, %v; want a match", check, err)
	}
	stale := &BotIdentity{Login: "old-app[bot]", UserID: 5, ResolvedAt: time.Unix(1, 0).UTC()}
	for name, cached := range map[string]*BotIdentity{"stale": stale, "missing": nil} {
		p := mustProvider(t, Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: app.PrivateKeyPEM, Cached: cached}, app.URL)
		check, err := p.VerifyIdentity(context.Background())
		if err != nil || check.Match || check.Refresh == nil {
			t.Fatalf("%s: VerifyIdentity() = %+v, %v; want a refresh", name, check, err)
		}
		settings := ProfileSettings{AppID: githubtest.AppID, InstallationID: installationID, PrivateKey: mustKeyRef(t), Identity: cached}
		before, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		after, err := check.Refresh(before)
		if err != nil {
			t.Fatalf("%s: Refresh: %v", name, err)
		}
		decoded, err := DecodeProfileSettings(host, after)
		if err != nil || decoded.Identity == nil || decoded.Identity.Login != githubtest.BotLogin() || decoded.Identity.UserID != githubtest.BotUserID {
			t.Errorf("%s: refreshed block = %s, %v; want the bot as the identity", name, after, err)
		}
		decoded.Identity = settings.Identity
		if again, _ := json.Marshal(decoded); string(again) != string(before) {
			t.Errorf("%s: Refresh changed a member other than identity", name)
		}
	}
}

func TestRefreshRejectsAnInvalidBlock(t *testing.T) {
	_, err := refreshIdentity(json.RawMessage(`{"app_id":0}`), BotIdentity{})
	if err == nil || strings.Contains(err.Error(), "ghs_") {
		t.Errorf("refreshIdentity of an invalid block: %v, want an error", err)
	}
}

func mustKeyRef(t *testing.T) profile.KeyRef {
	t.Helper()
	ref, err := profile.ParseKeyRef("file:~/.config/agent-id/keys/claude.pem")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestFromProfile(t *testing.T) {
	app := newFakeApp(t)
	ref := mustKeyRef(t)
	block, err := json.Marshal(ProfileSettings{AppID: githubtest.AppID, InstallationID: installationID, PrivateKey: ref})
	if err != nil {
		t.Fatal(err)
	}
	loaded := 0
	load := func(got profile.KeyRef) ([]byte, error) {
		loaded++
		if got != ref {
			t.Errorf("loadKey got %v, want the profile's reference", got)
		}
		return append([]byte(nil), app.PrivateKeyPEM...), nil
	}
	p, err := FromProfile(profile.Profile{Name: "claude", Provider: "github", Host: "github.com", Settings: block}, load)
	if err != nil || p.TokenSubject() != "app-424242-installation-77" || loaded != 1 {
		t.Fatalf("FromProfile = %v, %v after %d key loads", p, err, loaded)
	}
	sentinel := errors.New("no key")
	_, err = FromProfile(profile.Profile{Name: "claude", Provider: "github", Host: "github.com", Settings: block}, func(profile.KeyRef) ([]byte, error) { return nil, sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("FromProfile with a failing loader: %v, want the loader's error", err)
	}
	if _, err := FromProfile(profile.Profile{Name: "claude", Provider: "github", Host: "gitlab.com", Settings: block}, load); !errors.Is(err, profile.ErrUnsupportedHost) {
		t.Errorf("FromProfile on another host: %v, want ErrUnsupportedHost", err)
	}
}
