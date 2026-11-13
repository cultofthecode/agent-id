package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// The fake provider "acme": its block is any JSON object, on one host.
const (
	acmeName   = "acme"
	acmePrefix = "acm_"
)

type acmeValidator struct{}

func (acmeValidator) ValidateSettings(host string, settings json.RawMessage) error {
	if host != providertest.FakeHost {
		return profile.Errorf(profile.ErrUnsupportedHost, "host: %s is not supported", profile.Quote(host))
	}
	var block map[string]any
	if err := json.Unmarshal(settings, &block); err != nil || len(block) == 0 {
		return profile.Errorf(profile.ErrConfigInvalid, "acme: must be a non-empty object")
	}
	return nil
}

var acmeFields = []provider.SetupField{{Name: "app-id", Usage: "the app number"}, {Name: "private-key", Usage: "the key file"}}

func acmeResult(repositories ...string) provider.SetupResult {
	return provider.SetupResult{
		Host:         providertest.FakeHost,
		Settings:     json.RawMessage(`{"app_id":7,"identity":"acme-bot"}`),
		Account:      "acme-bot[bot]",
		Identity:     provider.Identity{Name: "acme-bot[bot]", Email: "7+acme-bot[bot]@" + providertest.FakeHost},
		Details:      []provider.SetupDetail{{Label: "App", Value: "7"}, {Label: "Private key", Value: "file:/keys/acme.pem"}},
		Repositories: repositories,
	}
}

// fixture is a temporary profile store with the acme provider and a fake
// acme onboarder.
type fixture struct {
	store     *profile.Store
	onboarder *providertest.FakeOnboarder
	handler   *Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	testenv.Isolate(t)
	registry, err := profile.NewRegistry(profile.Registration{Name: acmeName, Validator: acmeValidator{}, TokenPrefixes: []string{acmePrefix}})
	if err != nil {
		t.Fatal(err)
	}
	path, err := profile.Path(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: profile.NewStore(path, registry), onboarder: providertest.NewFakeOnboarder(acmeName, acmeFields, acmeResult("o/a", "o/b"))}
	f.handler = &Handler{Onboarders: []provider.Onboarder{f.onboarder}, OpenStore: func() (Store, error) { return f.store, nil }}
	return f
}

func validInput() Input {
	return Input{Profile: "claude", Provider: acmeName, Values: map[string]string{"app-id": "7", "private-key": "/keys/acme.pem"}, Dir: "/work"}
}

// writeConfig writes a configuration document with mode 0600.
func (f *fixture) writeConfig(t *testing.T, doc string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.store.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.store.Path(), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshot returns the content hash of every file under the configuration
// directory but the lock file, which Write may create, or nil when the
// directory does not exist.
func (f *fixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	dir := filepath.Dir(f.store.Path())
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) != ".lock" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return files
}

const existingConfig = `{"version":1,"profiles":{
  "claude":{"provider":"acme","host":"git.agent-id.test","acme":{"app_id":1,"identity":"old-bot"}},
  "other":{"provider":"acme","host":"git.agent-id.test","acme":{"app_id":3}},
  "gl-bot":{"provider":"gitlab","host":"gitlab.com","gitlab":{"group":"g","nested":{"a":[1,2]}}}
}}`

func TestHandleCreatesTheProfile(t *testing.T) {
	f := newFixture(t)
	out, err := f.handler.Handle(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := Output{
		Profile: "claude", Provider: acmeName, Host: providertest.FakeHost, Account: "acme-bot[bot]",
		Identity: acmeResult().Identity, Details: acmeResult().Details, Repositories: []string{"o/a", "o/b"},
		Created: true, ConfigPath: f.store.Path(),
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("output = %+v, want %+v", out, want)
	}
	p, err := f.store.Resolve("claude")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Provider != acmeName || p.Host != providertest.FakeHost || string(p.Settings) != string(acmeResult().Settings) {
		t.Errorf("stored profile = %+v", p)
	}
	info, err := os.Stat(f.store.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, %v; want 0600", info, err)
	}
	if requests := f.onboarder.Requests(); len(requests) != 1 || requests[0].Dir != "/work" || requests[0].Values["app-id"] != "7" {
		t.Errorf("onboarder requests = %+v", requests)
	}
}

func TestHandleUpdatesTheProfileAndKeepsTheOthers(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, existingConfig)
	before, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.handler.Handle(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Created {
		t.Errorf("Created = true, want updated")
	}
	after, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Profiles["claude"].Settings) != string(acmeResult().Settings) {
		t.Errorf("claude = %s", after.Profiles["claude"].Settings)
	}
	for _, name := range []string{"other", "gl-bot"} {
		if !reflect.DeepEqual(after.Profiles[name], before.Profiles[name]) {
			t.Errorf("profile %s changed: %+v → %+v", name, before.Profiles[name], after.Profiles[name])
		}
	}
}

func TestHandleRefusesAnotherProvidersProfileBeforeTheNetwork(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, strings.Replace(existingConfig, `"claude":{"provider":"acme","host":"git.agent-id.test","acme":{"app_id":1,"identity":"old-bot"}}`, `"claude":{"provider":"gitlab","host":"gitlab.com","gitlab":{"x":1}}`, 1))
	before := f.snapshot(t)
	_, err := f.handler.Handle(context.Background(), validInput())
	if !errors.Is(err, ErrProviderConflict) {
		t.Fatalf("error = %v, want ErrProviderConflict", err)
	}
	for _, want := range []string{`profile "claude" already exists for provider "gitlab"`, "choose another name or remove it from " + f.store.Path()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if len(f.onboarder.Requests()) != 0 {
		t.Errorf("the onboarder was called")
	}
	if !reflect.DeepEqual(f.snapshot(t), before) {
		t.Errorf("the configuration changed")
	}
}

func TestHandleRefusesAConflictThatAppearsDuringTheNetworkPhase(t *testing.T) {
	f := newFixture(t)
	var before []byte
	f.onboarder.OnOnboard(func() {
		err := f.store.Write(func(c *profile.Config) error {
			c.Profiles["claude"] = profile.Profile{Name: "claude", Provider: "gitlab", Host: "gitlab.com", Settings: json.RawMessage(`{"x":1}`)}
			return nil
		})
		if err != nil {
			t.Errorf("concurrent write: %v", err)
		}
		before, _ = os.ReadFile(f.store.Path())
	})
	_, err := f.handler.Handle(context.Background(), validInput())
	if !errors.Is(err, ErrProviderConflict) {
		t.Fatalf("error = %v, want ErrProviderConflict", err)
	}
	after, _ := os.ReadFile(f.store.Path())
	if !bytes.Equal(before, after) {
		t.Errorf("the configuration changed after the conflict")
	}
}

func TestHandleStopsOnStoreErrorsBeforeTheNetwork(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
		class error
	}{
		{"group-writable file", func(t *testing.T, f *fixture) {
			f.writeConfig(t, existingConfig)
			if err := os.Chmod(f.store.Path(), 0o664); err != nil {
				t.Fatal(err)
			}
		}, profile.ErrInsecurePermissions},
		{"invalid JSON", func(t *testing.T, f *fixture) { f.writeConfig(t, `{"version":1,`) }, profile.ErrConfigInvalid},
		{"newer version", func(t *testing.T, f *fixture) { f.writeConfig(t, `{"version":2,"profiles":{}}`) }, profile.ErrUnsupportedVersion},
		{"symlink to a missing file", func(t *testing.T, f *fixture) {
			if err := os.MkdirAll(filepath.Dir(f.store.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(filepath.Dir(f.store.Path()), "gone.json"), f.store.Path()); err != nil {
				t.Fatal(err)
			}
		}, profile.ErrConfigNotFound},
		{"relative configuration path", func(t *testing.T, f *fixture) {
			f.store = profile.NewStore("rel/config.json", nil)
		}, profile.ErrConfigNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(t, f)
			before := f.snapshot(t)
			_, err := f.handler.Handle(context.Background(), validInput())
			if !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want class %v", err, tt.class)
			}
			if len(f.onboarder.Requests()) != 0 {
				t.Errorf("the onboarder was called")
			}
			if !reflect.DeepEqual(f.snapshot(t), before) {
				t.Errorf("the configuration changed")
			}
		})
	}
}

func TestHandleOpenStoreFailure(t *testing.T) {
	f := newFixture(t)
	f.handler.OpenStore = func() (Store, error) {
		return nil, profile.Errorf(profile.ErrConfigNotFound, "no agent-id configuration: neither XDG_CONFIG_HOME nor HOME is an absolute path")
	}
	_, err := f.handler.Handle(context.Background(), validInput())
	if !errors.Is(err, profile.ErrConfigNotFound) || len(f.onboarder.Requests()) != 0 {
		t.Fatalf("error = %v (requests %d), want ErrConfigNotFound and no request", err, len(f.onboarder.Requests()))
	}
}

func TestHandleWritesNothingWhenTheOnboarderFails(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing file %v", existing), func(t *testing.T) {
			f := newFixture(t)
			if existing {
				f.writeConfig(t, existingConfig)
			}
			before := f.snapshot(t)
			f.onboarder.Fail(fmt.Errorf("installation 9 not found: %w", provider.ErrNotFound))
			_, err := f.handler.Handle(context.Background(), validInput())
			if !errors.Is(err, provider.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if !reflect.DeepEqual(f.snapshot(t), before) {
				t.Errorf("the configuration changed: %v → %v", before, f.snapshot(t))
			}
		})
	}
}

func TestHandleWritesNothingWhenTheResultDoesNotValidate(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, existingConfig)
	before := f.snapshot(t)
	result := acmeResult()
	result.Settings = json.RawMessage(`{}`)
	f.onboarder.SetResult(result)
	_, err := f.handler.Handle(context.Background(), validInput())
	if !errors.Is(err, profile.ErrConfigInvalid) {
		t.Fatalf("error = %v, want ErrConfigInvalid", err)
	}
	if !reflect.DeepEqual(f.snapshot(t), before) {
		t.Errorf("the configuration changed")
	}
}

func TestHandleRefusesInputWithoutIO(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	tests := []struct {
		name   string
		change func(in *Input)
		class  error
		want   string
		secret string
	}{
		{"profile missing", func(in *Input) { in.Profile = "" }, ErrProfileRequired, "--profile is required", ""},
		{"all flags missing", func(in *Input) { in.Profile = ""; in.Values = map[string]string{} }, ErrMissingFlags, "missing required flags: --profile (the agent profile to create or update), --app-id (the app number), --private-key (the key file)", ""},
		{"two provider flags missing", func(in *Input) { in.Values = map[string]string{} }, ErrMissingFlags, "missing required flags: --app-id (the app number), --private-key (the key file)", ""},
		{"unknown provider", func(in *Input) { in.Provider = "gitlab" }, ErrUnknownProvider, `unknown provider "gitlab"`, ""},
		{"uppercase profile", func(in *Input) { in.Profile = "Claude" }, profile.ErrConfigInvalid, `invalid profile name "Claude": use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"`, ""},
		{"profile ending with a dash", func(in *Input) { in.Profile = "claude-" }, profile.ErrConfigInvalid, `invalid profile name "claude-"`, ""},
		{"token as profile", func(in *Input) { in.Profile = acmePrefix + "secret" }, profile.ErrInlineSecret, "--profile looks like a token or key; it is not shown — remove it and revoke it if it was real", acmePrefix + "secret"},
		{"PEM as profile", func(in *Input) { in.Profile = "-----BEGIN RSA PRIVATE KEY-----" }, profile.ErrInlineSecret, "--profile looks like a token or key", "BEGIN"},
		{"JWT as profile", func(in *Input) { in.Profile = jwt }, profile.ErrInlineSecret, "--profile looks like a token or key", jwt},
		{"fake token as key", func(in *Input) { in.Values["private-key"] = testenv.FakeToken("pasted") }, providertest.ErrFakeValue, "--private-key looks like a token or key", testenv.FakeToken("pasted")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			in := validInput()
			tt.change(&in)
			_, err := f.handler.Handle(context.Background(), in)
			if !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want class %v", err, tt.class)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Errorf("error %q echoes the secret", err)
			}
			if len(f.onboarder.Requests()) != 0 || f.snapshot(t) != nil {
				t.Errorf("the input reached the onboarder or the file system")
			}
		})
	}
}

func TestRender(t *testing.T) {
	testenv.Isolate(t)
	out := Output{
		Profile: "claude", Provider: "github", Host: "github.com", Account: "claude-dev[bot]",
		Identity:     provider.Identity{Name: "claude-dev[bot]", Email: "12345678+claude-dev[bot]@users.noreply.github.com"},
		Details:      []provider.SetupDetail{{Label: "App", Value: "123456"}, {Label: "Installation", Value: "789012"}, {Label: "Private key", Value: "file:/Users/dev/keys/claude.pem"}},
		Repositories: []string{"octo/api", "octo/web"},
		Created:      true,
		ConfigPath:   "/Users/dev/.config/agent-id/config.json",
	}
	want := `Profile:       claude (created)
Provider:      github (github.com)
App:           123456
Installation:  789012
Private key:   file:/Users/dev/keys/claude.pem
Identity:      claude-dev[bot]
Commit email:  12345678+claude-dev[bot]@users.noreply.github.com
Repositories:  2
               octo/api
               octo/web
Config:        /Users/dev/.config/agent-id/config.json

Next: agent-id exec --profile claude -- <command>
`
	var b bytes.Buffer
	if err := Render(&b, out); err != nil {
		t.Fatal(err)
	}
	if b.String() != want {
		t.Errorf("Render =\n%s\nwant\n%s", b.String(), want)
	}
}

func TestRenderRepositoryCounts(t *testing.T) {
	testenv.Isolate(t)
	many := make([]string, 25)
	for i := range many {
		many[i] = fmt.Sprintf("o/r%02d", i)
	}
	var b bytes.Buffer
	if err := Render(&b, Output{Repositories: many}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Repositories:  25\n") || !strings.Contains(b.String(), "               o/r19\n               … and 5 more\n") || strings.Contains(b.String(), "o/r20") {
		t.Errorf("Render of 25 repositories =\n%s", b.String())
	}
	b.Reset()
	if err := Render(&b, Output{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Repositories:  0 (the installation can access no repository; add repositories in the installation's settings)\n") {
		t.Errorf("Render of no repository =\n%s", b.String())
	}
	if !strings.Contains(b.String(), "Profile:        (updated)\n") {
		t.Errorf("Render of an update =\n%s", b.String())
	}
}

func TestRenderReplacesControlCharacters(t *testing.T) {
	testenv.Isolate(t)
	var b bytes.Buffer
	out := Output{Profile: "claude", Details: []provider.SetupDetail{{Label: "Key\x1b", Value: "file:/k\x1b]0;evil\x07\u202e.pem"}}, Repositories: []string{"o/a\nb"}}
	if err := Render(&b, out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(b.String(), "\x1b\x07\u202e") || strings.Contains(b.String(), "o/a\nb") {
		t.Errorf("Render printed a control character: %q", b.String())
	}
	if !strings.Contains(b.String(), "Key?:          file:/k?]0;evil??.pem\n") {
		t.Errorf("Render = %q", b.String())
	}
}

func TestCommand(t *testing.T) {
	f := newFixture(t)
	run := func(args ...string) (string, error) {
		cmd := NewCommand(f.handler)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(context.Background())
		return out.String(), err
	}
	help, err := run("acme", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, flag := range []string{"--profile", "--app-id", "--private-key", "the key file"} {
		if !strings.Contains(help, flag) {
			t.Errorf("help does not list %s:\n%s", flag, help)
		}
	}
	if _, err := run("acme", "--app-id", "7"); err == nil || !strings.HasPrefix(err.Error(), "setup acme: missing required flags: --profile") {
		t.Errorf("missing flags: %v", err)
	}
	out, err := run("acme", "--profile", "claude", "--app-id", "7", "--private-key", "keys/acme.pem")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !strings.Contains(out, "Profile:       claude (created)") || !strings.Contains(out, "Next: agent-id exec --profile claude -- <command>") {
		t.Errorf("output =\n%s", out)
	}
	wd, _ := os.Getwd()
	if requests := f.onboarder.Requests(); len(requests) != 1 || requests[0].Dir != wd || requests[0].Values["private-key"] != "keys/acme.pem" {
		t.Errorf("requests = %+v, want the working directory %s", requests, wd)
	}
	if _, err := run("gitlab"); err == nil {
		t.Errorf("an unknown provider subcommand succeeded")
	}
}
