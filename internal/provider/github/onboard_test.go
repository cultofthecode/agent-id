package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// onboardFixture is a fake GitHub App with installationID on two
// repositories, its key in a 0600 file, and an onboarder that reaches the
// fake and loads keys through the credential source, counting its calls.
type onboardFixture struct {
	env       testenv.Env
	fake      *testenv.FakeAPI
	app       *githubtest.App
	keyPath   string
	loads     int
	onboarder *Onboarder
}

func newOnboardFixture(t *testing.T, repositories ...string) *onboardFixture {
	t.Helper()
	f := &onboardFixture{env: testenv.Isolate(t)}
	f.fake = testenv.NewFakeAPI(t)
	f.app = githubtest.NewApp(t, f.fake)
	f.app.AddInstallation(installationID, repositories...)
	f.keyPath = writeKey(t, f.env, "app.pem", f.app.PrivateKeyPEM, 0o600)
	f.onboarder = &Onboarder{
		loadKey: func(ref profile.KeyRef) ([]byte, error) {
			f.loads++
			path, err := ref.Path(os.Getenv("HOME"))
			if err != nil {
				return nil, err
			}
			return credential.File{Path: path}.PrivateKeyPEM()
		},
		api: api{baseURL: f.fake.URL, client: testenv.NewHTTPClient(t)},
	}
	return f
}

// request returns the setup request for the fake App with the key file.
func (f *onboardFixture) request() provider.SetupRequest {
	return provider.SetupRequest{
		Values: map[string]string{
			fieldAppID:          fmt.Sprint(githubtest.AppID),
			fieldInstallationID: fmt.Sprint(installationID),
			fieldPrivateKey:     f.keyPath,
		},
		Dir: f.env.Root,
	}
}

// calls counts the key loads and the requests to the fake.
func (f *onboardFixture) calls() int {
	return f.loads + len(f.fake.Requests())
}

// writeKey writes a key file named name with mode under the test root.
func writeKey(t *testing.T, env testenv.Env, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(env.Root, "keys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// otherKey returns the PEM of an RSA key that the fake App does not know.
func otherKey(t *testing.T, bits int) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// checkNoSetupSecret fails when text holds an issued token, a token prefix,
// a PEM marker, or a JWT.
func checkNoSetupSecret(t *testing.T, app *githubtest.App, text string) {
	t.Helper()
	tokens := []string{}
	for _, token := range app.Tokens() {
		tokens = append(tokens, token.Value)
	}
	checkNoSecret(t, text, tokens...)
	if strings.Contains(text, "ghs_") || strings.Contains(text, pemMarker) {
		t.Errorf("%q holds a token prefix or a PEM marker", text)
	}
}

func TestOnboardWritesAVerifiedProfile(t *testing.T) {
	f := newOnboardFixture(t, "o/b", "o/a")
	before := time.Now().UTC().Truncate(time.Second)
	result, err := f.onboarder.Onboard(context.Background(), f.request())
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if len(f.app.Tokens()) != 1 {
		t.Errorf("tokens minted = %d, want 1", len(f.app.Tokens()))
	}
	login := githubtest.BotLogin()
	wantEmail := fmt.Sprintf("%d+%s@users.noreply.github.com", githubtest.BotUserID, login)
	if result.Host != "github.com" || result.Account != login || result.Identity.Name != login || result.Identity.Email != wantEmail {
		t.Errorf("result = %+v", result)
	}
	if !reflect.DeepEqual(result.Repositories, []string{"o/a", "o/b"}) {
		t.Errorf("repositories = %v, want [o/a o/b]", result.Repositories)
	}
	wantDetails := []provider.SetupDetail{
		{Label: "App", Value: fmt.Sprint(githubtest.AppID)},
		{Label: "Installation", Value: fmt.Sprint(installationID)},
		{Label: "Private key", Value: "file:" + f.keyPath},
	}
	if !reflect.DeepEqual(result.Details, wantDetails) {
		t.Errorf("details = %+v, want %+v", result.Details, wantDetails)
	}
	checkNoSetupSecret(t, f.app, fmt.Sprintf("%+v %s", result, result.Settings))

	registry, err := profile.NewRegistry(ProfileRegistration())
	if err != nil {
		t.Fatal(err)
	}
	path, err := profile.Path(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	store := profile.NewStore(path, registry)
	err = store.Write(func(c *profile.Config) error {
		c.Profiles["claude"] = profile.Profile{Name: "claude", Provider: f.onboarder.Name(), Host: result.Host, Settings: result.Settings}
		return nil
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	stored, err := store.Resolve("claude")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	settings, err := DecodeProfileSettings(stored.Host, stored.Settings)
	if err != nil {
		t.Fatalf("DecodeProfileSettings: %v", err)
	}
	if settings.AppID != githubtest.AppID || settings.InstallationID != installationID || settings.PrivateKey.String() != "file:"+f.keyPath {
		t.Errorf("stored settings = %+v", settings)
	}
	if settings.Identity == nil || settings.Identity.Login != login || settings.Identity.UserID != githubtest.BotUserID {
		t.Fatalf("stored identity = %+v", settings.Identity)
	}
	resolved := settings.Identity.ResolvedAt
	if resolved.Location() != time.UTC || resolved.Before(before) || resolved.After(time.Now()) || resolved.Nanosecond() != 0 {
		t.Errorf("resolved_at = %v, want UTC seconds between %v and now", resolved, before)
	}
}

func TestOnboardRepositoryCounts(t *testing.T) {
	many := make([]string, 25)
	for i := range many {
		many[i] = fmt.Sprintf("o/r%02d", 24-i)
	}
	for _, repositories := range [][]string{nil, many} {
		t.Run(fmt.Sprint(len(repositories)), func(t *testing.T) {
			f := newOnboardFixture(t, repositories...)
			result, err := f.onboarder.Onboard(context.Background(), f.request())
			if err != nil {
				t.Fatalf("Onboard: %v", err)
			}
			if len(result.Repositories) != len(repositories) {
				t.Fatalf("repositories = %d, want %d", len(result.Repositories), len(repositories))
			}
			if len(repositories) > 0 && (result.Repositories[0] != "o/r00" || result.Repositories[24] != "o/r24") {
				t.Errorf("repositories are not sorted: %v", result.Repositories)
			}
		})
	}
}

func TestOnboardTokenFailures(t *testing.T) {
	tests := []struct {
		status int
		class  error
		want   []string
	}{
		{401, provider.ErrUnauthorized, []string{"GitHub rejected the App credentials: check that --app-id 424242 is the App's ID and that --private-key belongs to that App"}},
		{403, provider.ErrForbidden, []string{"GitHub denied the App (suspended or blocked)"}},
		{404, provider.ErrNotFound, []string{fmt.Sprintf("installation %d of App 424242 not found: check --installation-id", installationID)}},
		{429, provider.ErrRateLimited, []string{"GitHub is rate limiting this machine; try again later"}},
		{503, provider.ErrUnavailable, []string{"cannot reach GitHub: github: create an installation token: HTTP 503"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			f := newOnboardFixture(t, "o/a")
			f.app.FailTokens(tt.status)
			_, err := f.onboarder.Onboard(context.Background(), f.request())
			if !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want class %v", err, tt.class)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			checkNoSetupSecret(t, f.app, err.Error())
		})
	}
}

func TestOnboardKeyOfAnotherApp(t *testing.T) {
	f := newOnboardFixture(t, "o/a")
	r := f.request()
	r.Values[fieldPrivateKey] = writeKey(t, f.env, "other.pem", otherKey(t, 2048), 0o600)
	_, err := f.onboarder.Onboard(context.Background(), r)
	if !errors.Is(err, provider.ErrUnauthorized) || !strings.Contains(err.Error(), "GitHub rejected the App credentials") || !strings.Contains(err.Error(), "--app-id") || !strings.Contains(err.Error(), "--private-key") {
		t.Fatalf("error = %v, want the rejected-credentials message", err)
	}
	if len(f.app.Tokens()) != 0 {
		t.Errorf("a token was minted")
	}
	checkNoSetupSecret(t, f.app, err.Error())
}

func TestOnboardKeyFailuresReachNoGitHub(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *onboardFixture) string
		class error
		want  string
	}{
		{"key file too open", func(t *testing.T, f *onboardFixture) string {
			return writeKey(t, f.env, "open.pem", f.app.PrivateKeyPEM, 0o644)
		}, credential.ErrInsecureKeyFile, "chmod 600"},
		{"key file missing", func(t *testing.T, f *onboardFixture) string {
			return filepath.Join(f.env.Root, "keys", "missing.pem")
		}, credential.ErrKeyNotFound, "missing.pem"},
		{"short key", func(t *testing.T, f *onboardFixture) string {
			return writeKey(t, f.env, "short.pem", otherKey(t, 1024), 0o600)
		}, ErrInvalidKey, "not a GitHub App private key (one unencrypted RSA key of at least 2048 bits, PEM)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newOnboardFixture(t, "o/a")
			r := f.request()
			r.Values[fieldPrivateKey] = tt.setup(t, f)
			_, err := f.onboarder.Onboard(context.Background(), r)
			if !errors.Is(err, tt.class) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want class %v containing %q", err, tt.class, tt.want)
			}
			if requests := f.fake.Requests(); len(requests) != 0 {
				t.Errorf("GitHub received %v", requests)
			}
			checkNoSetupSecret(t, f.app, err.Error())
		})
	}
}

func TestOnboardHostileAnswers(t *testing.T) {
	tests := []struct {
		name   string
		routes func(fake *testenv.FakeAPI)
	}{
		{"bot login with an escape", func(fake *testenv.FakeAPI) {
			fake.Handle("POST /app/installations/{id}/access_tokens", answer(201, tokenBody(testenv.FakeToken("hostile"))))
			fake.Handle("GET /app", answer(200, map[string]any{"slug": "evil\x1b[31m"}))
		}},
		{"repository with an escape", func(fake *testenv.FakeAPI) {
			fake.Handle("POST /app/installations/{id}/access_tokens", answer(201, tokenBody(testenv.FakeToken("hostile"))))
			fake.Handle("GET /app", answer(200, map[string]any{"slug": "ok-app"}))
			fake.Handle("GET /users/{login}", answer(200, map[string]any{"login": "ok-app[bot]", "id": 5, "type": "Bot"}))
			fake.Handle("GET /installation/repositories", answer(200, map[string]any{"repositories": []any{map[string]any{"name": "a\x1b]0;x", "owner": map[string]any{"login": "o"}}}}))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, p := newHostileAPI(t)
			tt.routes(fake)
			values := setupValues{appID: githubtest.AppID, installationID: installationID, key: profile.KeyRef{Scheme: profile.SchemeFile, Value: "/k.pem"}}
			bot, repositories, err := verify(context.Background(), p, values)
			if !errors.Is(err, provider.ErrUnavailable) || !strings.HasPrefix(err.Error(), "cannot reach GitHub: ") {
				t.Fatalf("verify = %v, %v, %v; want ErrUnavailable", bot, repositories, err)
			}
			if strings.ContainsRune(err.Error(), 0x1b) {
				t.Errorf("error %q holds an escape", err)
			}
			checkNoSecret(t, err.Error(), testenv.FakeToken("hostile"))
		})
	}
}

func TestOnboardCanceled(t *testing.T) {
	f := newOnboardFixture(t, "o/a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.onboarder.Onboard(ctx, f.request())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestParseSetupValues(t *testing.T) {
	testenv.Isolate(t)
	valid := map[string]string{fieldAppID: "123456", fieldInstallationID: "789012", fieldPrivateKey: "/keys/claude.pem"}
	with := func(field, value string) map[string]string {
		values := map[string]string{}
		for k, v := range valid {
			values[k] = v
		}
		values[field] = value
		return values
	}
	accepted := []struct {
		name  string
		value string
		dir   string
		want  string
	}{
		{"absolute path", "/keys/claude.pem", "/work", "file:/keys/claude.pem"},
		{"uncleaned absolute path", "/keys/../keys/./claude.pem", "/work", "file:/keys/claude.pem"},
		{"home path", "~/k.pem", "/work", "file:~/k.pem"},
		{"relative path", "rel/k.pem", "/work", "file:/work/rel/k.pem"},
		{"relative path upwards", "../k.pem", "/work/sub", "file:/work/k.pem"},
		{"file reference", "file:/keys/k.pem", "/work", "file:/keys/k.pem"},
	}
	for _, tt := range accepted {
		values, err := parseSetupValues(with(fieldPrivateKey, tt.value), tt.dir)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if values.key.String() != tt.want || values.appID != 123456 || values.installationID != 789012 {
			t.Errorf("%s: %+v, want key %s", tt.name, values, tt.want)
		}
	}
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxIn0.c2lnbmF0dXJl"
	refused := []struct {
		name   string
		values map[string]string
		dir    string
		class  error
		want   string
		secret string
	}{
		{"app-id missing", with(fieldAppID, ""), "/w", ErrSetupValue, "--app-id is required (the App ID on the App's settings page)", ""},
		{"installation-id missing", with(fieldInstallationID, ""), "/w", ErrSetupValue, "--installation-id is required", ""},
		{"private-key missing", with(fieldPrivateKey, ""), "/w", ErrSetupValue, "--private-key is required (the .pem file downloaded from the App's settings)", ""},
		{"app-id zero", with(fieldAppID, "0"), "/w", ErrSetupValue, "--app-id must be a positive integer (the App ID on the App's settings page)", ""},
		{"app-id negative", with(fieldAppID, "-1"), "/w", ErrSetupValue, "--app-id must be a positive integer", ""},
		{"app-id leading zero", with(fieldAppID, "0123"), "/w", ErrSetupValue, "--app-id must be a positive integer", ""},
		{"app-id exponent", with(fieldAppID, "1e6"), "/w", ErrSetupValue, "--app-id must be a positive integer", ""},
		{"app-id 2^53", with(fieldAppID, "9007199254740992"), "/w", ErrSetupValue, "--app-id must be a positive integer", ""},
		{"app-id whitespace", with(fieldAppID, " 1"), "/w", ErrSetupValue, "--app-id must be a positive integer", ""},
		{"installation-id word", with(fieldInstallationID, "abc"), "/w", ErrSetupValue, "--installation-id must be a positive integer (the number at the end of the installation's settings URL)", ""},
		{"relative file reference", with(fieldPrivateKey, "file:rel/k.pem"), "/w", profile.ErrUnsupportedKeyReference, "--private-key: a file reference needs an absolute path or ~/…", "rel/k.pem"},
		{"keyring reference", with(fieldPrivateKey, "keyring:claude"), "/w", profile.ErrUnsupportedKeyReference, "--private-key: keyring references are reserved", "claude"},
		{"relative path without a directory", with(fieldPrivateKey, "k.pem"), "", ErrSetupValue, "--private-key is a relative path, but the working directory is unknown", ""},
		{"path with a newline", with(fieldPrivateKey, "/keys/a\nb.pem"), "/w", ErrSetupValue, "--private-key contains a control character; it is not shown", "a\nb"},
		{"path with a bidi override", with(fieldPrivateKey, "/keys/a\u202eb.pem"), "/w", ErrSetupValue, "--private-key contains a control character", "\u202e"},
		{"PEM header as key", with(fieldPrivateKey, "-----BEGIN RSA PRIVATE KEY-----"), "/w", ErrSetupValue, "--private-key looks like a token or key; it is not shown — remove it and revoke it if it was real", "BEGIN"},
		{"fake token as key", with(fieldPrivateKey, testenv.FakeToken("pasted")), "/w", ErrSetupValue, "--private-key looks like a token or key", testenv.FakeToken("pasted")},
		{"personal token as key", with(fieldPrivateKey, "ghp_x"), "/w", ErrSetupValue, "--private-key looks like a token or key", "ghp_x"},
		{"fine-grained token as key", with(fieldPrivateKey, "github_pat_x"), "/w", ErrSetupValue, "--private-key looks like a token or key", "github_pat_x"},
		{"JWT as key", with(fieldPrivateKey, jwt), "/w", ErrSetupValue, "--private-key looks like a token or key", jwt},
		{"token in a relative path", with(fieldPrivateKey, "dir/ghs_abc"), "/w", ErrSetupValue, "--private-key looks like a token or key", "ghs_abc"},
		{"token as app-id", with(fieldAppID, testenv.FakeToken("id")), "/w", ErrSetupValue, "--app-id looks like a token or key", testenv.FakeToken("id")},
	}
	for _, tt := range refused {
		_, err := parseSetupValues(tt.values, tt.dir)
		if !errors.Is(err, tt.class) {
			t.Errorf("%s: error = %v, want class %v", tt.name, err, tt.class)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %q, want it to contain %q", tt.name, err, tt.want)
		}
		if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
			t.Errorf("%s: error %q echoes the value", tt.name, err)
		}
	}
}

func TestOnboardCheckLoadsNothing(t *testing.T) {
	f := newOnboardFixture(t, "o/a")
	r := f.request()
	r.Values[fieldPrivateKey] = "-----BEGIN RSA PRIVATE KEY-----"
	if err := f.onboarder.Check(r); err == nil {
		t.Fatal("Check accepted a PEM header")
	}
	if _, err := f.onboarder.Onboard(context.Background(), r); err == nil {
		t.Fatal("Onboard accepted a PEM header")
	}
	if f.calls() != 0 {
		t.Errorf("calls to the outside = %d, want 0", f.calls())
	}
}

func TestOnboarderContract(t *testing.T) {
	f := newOnboardFixture(t, "o/a", "o/b")
	providertest.RunOnboarderContract(t, providertest.OnboarderSubject{Onboarder: f.onboarder, Valid: f.request(), Calls: f.calls}, ProfileRegistration().TokenPrefixes)
}

func TestNewOnboarder(t *testing.T) {
	testenv.Isolate(t)
	o := NewOnboarder(func(profile.KeyRef) ([]byte, error) { return nil, errors.New("no key") })
	if o.Name() != "github" || len(o.SetupFields()) != 3 {
		t.Errorf("onboarder = %s with %v", o.Name(), o.SetupFields())
	}
	if o.api.baseURL != "" || o.api.client != nil {
		t.Errorf("NewOnboarder sets a test hook")
	}
}
