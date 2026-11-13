package github

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// exampleConfig is the v1 example of the profile-config spec.
const exampleConfig = `{
  "version": 1,
  "profiles": {
    "claude": {
      "provider": "github",
      "host": "github.com",
      "github": {
        "app_id": 123456,
        "installation_id": 789012,
        "private_key": "file:~/.config/agent-id/keys/claude.pem",
        "identity": {
          "login": "claude-dev[bot]",
          "user_id": 12345678,
          "resolved_at": "2026-11-12T10:00:00Z"
        }
      }
    },
    "reviewer": {
      "provider": "github",
      "host": "github.com",
      "github": {
        "app_id": 456789,
        "installation_id": 987654,
        "private_key": "file:/Users/dev/keys/reviewer.pem"
      }
    },
    "gl-bot": {
      "provider": "gitlab",
      "host": "gitlab.com",
      "gitlab": { "anything": "defined by the GitLab adapter" }
    }
  }
}`

// productionRegistry returns the registry the composition root builds.
func productionRegistry(t *testing.T) *profile.Registry {
	t.Helper()
	registry, err := profile.NewRegistry(ProfileRegistration())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// configWithBlock returns a v1 configuration with one github profile
// "claude" on host whose block is block.
func configWithBlock(host, block string) string {
	return `{"version":1,"profiles":{"claude":{"provider":"github","host":"` + host + `","github":` + block + `}}}`
}

func TestExampleConfigDecodes(t *testing.T) {
	testenv.Isolate(t)
	config, err := profile.Parse([]byte(exampleConfig), productionRegistry(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	claude, err := DecodeProfileSettings(config.Profiles["claude"].Host, config.Profiles["claude"].Settings)
	if err != nil {
		t.Fatalf("DecodeProfileSettings(claude): %v", err)
	}
	want := ProfileSettings{
		AppID:          123456,
		InstallationID: 789012,
		PrivateKey:     profile.KeyRef{Scheme: profile.SchemeFile, Value: "~/.config/agent-id/keys/claude.pem"},
		Identity:       &BotIdentity{Login: "claude-dev[bot]", UserID: 12345678, ResolvedAt: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)},
	}
	if !reflect.DeepEqual(claude, want) {
		t.Errorf("claude = %+v, want %+v", claude, want)
	}
	identity, err := claude.Identity.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if identity.Name != "claude-dev[bot]" || identity.Email != "12345678+claude-dev[bot]@users.noreply.github.com" {
		t.Errorf("commit identity = %+v", identity)
	}
	reviewer, err := DecodeProfileSettings("github.com", config.Profiles["reviewer"].Settings)
	if err != nil {
		t.Fatalf("DecodeProfileSettings(reviewer): %v", err)
	}
	if reviewer.Identity != nil || reviewer.AppID != 456789 || reviewer.PrivateKey.String() != "file:/Users/dev/keys/reviewer.pem" {
		t.Errorf("reviewer = %+v", reviewer)
	}
	store := config.Profiles["gl-bot"]
	if store.Provider != "gitlab" {
		t.Errorf("gl-bot provider = %q", store.Provider)
	}
}

const validBlock = `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem"}`

func TestDecodeProfileSettingsRefuses(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		block     string
		class     error
		wantText  string
		forbidden string
	}{
		{"enterprise host", "github.example.com", validBlock, profile.ErrUnsupportedHost, `host: "github.example.com" is not supported by the github provider; this version accepts only github.com`, ""},
		{"api host", "api.github.com", validBlock, profile.ErrUnsupportedHost, "accepts only github.com", ""},
		{"gitlab host", "gitlab.com", validBlock, profile.ErrUnsupportedHost, "accepts only github.com", ""},
		{"not an object", "github.com", `[]`, profile.ErrConfigInvalid, "github: must be an object", ""},
		{"unknown member", "github.com", `{"app_id":1,"installation_id":2,"privat_key":"file:/k.pem"}`, profile.ErrConfigInvalid, `github: unknown member "privat_key"`, ""},
		{"app_id as string", "github.com", `{"app_id":"1","installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id zero", "github.com", `{"app_id":0,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id negative", "github.com", `{"app_id":-1,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id fraction", "github.com", `{"app_id":1.5,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id exponent", "github.com", `{"app_id":1e6,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id above 2^53-1", "github.com", `{"app_id":9007199254740992,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"app_id null", "github.com", `{"app_id":null,"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.app_id: must be a positive integer", ""},
		{"installation_id bool", "github.com", `{"app_id":1,"installation_id":true,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github.installation_id: must be a positive integer", ""},
		{"app_id missing", "github.com", `{"installation_id":2,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github: app_id is missing", ""},
		{"installation_id missing", "github.com", `{"app_id":1,"private_key":"file:/k.pem"}`, profile.ErrConfigInvalid, "github: installation_id is missing", ""},
		{"private_key missing", "github.com", `{"app_id":1,"installation_id":2}`, profile.ErrConfigInvalid, "github: private_key is missing", ""},
		{"private_key not a string", "github.com", `{"app_id":1,"installation_id":2,"private_key":7}`, profile.ErrUnsupportedKeyReference, "github.private_key: must be a string", ""},
		{"keyring reference", "github.com", `{"app_id":1,"installation_id":2,"private_key":"keyring:claude-github-app"}`, profile.ErrUnsupportedKeyReference, "github.private_key: keyring references are reserved and not supported yet", "claude-github-app"},
		{"relative file reference", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:keys/claude.pem"}`, profile.ErrUnsupportedKeyReference, "github.private_key: a file reference needs an absolute path or ~/", "keys/claude.pem"},
		{"bare path", "github.com", `{"app_id":1,"installation_id":2,"private_key":"/k/secret-name.pem"}`, profile.ErrUnsupportedKeyReference, "github.private_key: unknown key reference", "secret-name"},
		{"identity not an object", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":"x"}`, profile.ErrConfigInvalid, "github.identity: must be an object", ""},
		{"identity login missing", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, "github.identity: login is missing", ""},
		{"identity user_id missing", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, "github.identity: user_id is missing", ""},
		{"identity resolved_at missing", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","user_id":3}}`, profile.ErrConfigInvalid, "github.identity: resolved_at is missing", ""},
		{"identity unknown member", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","user_id":3,"resolved_at":"2026-11-12T10:00:00Z","email":"x"}}`, profile.ErrConfigInvalid, `github.identity: unknown member "email"`, ""},
		{"login without [bot]", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"claude-dev","user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, `github.identity.login: "claude-dev" is not a GitHub App bot login (expected "<slug>[bot]")`, ""},
		{"login uppercase", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"Claude[bot]","user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, "is not a GitHub App bot login", ""},
		{"login with an escape", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a\u001b[31m[bot]","user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, `"a?[31m[bot]" is not a GitHub App bot login`, "\x1b"},
		{"login not a string", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":5,"user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, "github.identity.login: must be a string", ""},
		{"user_id string", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","user_id":"3","resolved_at":"2026-11-12T10:00:00Z"}}`, profile.ErrConfigInvalid, "github.identity.user_id: must be a positive integer", ""},
		{"resolved_at with an offset", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","user_id":3,"resolved_at":"2026-11-12T11:00:00+01:00"}}`, profile.ErrConfigInvalid, "github.identity.resolved_at: must be an RFC 3339 UTC timestamp", ""},
		{"resolved_at not a time", "github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","identity":{"login":"a[bot]","user_id":3,"resolved_at":"yesterday Z"}}`, profile.ErrConfigInvalid, "github.identity.resolved_at: must be an RFC 3339 UTC timestamp", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			_, err := DecodeProfileSettings(tt.host, json.RawMessage(tt.block))
			if !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want class %v", err, tt.class)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantText)
			}
			if tt.forbidden != "" && strings.Contains(err.Error(), tt.forbidden) {
				t.Errorf("error = %q echoes %q", err, tt.forbidden)
			}
		})
	}
}

func TestLookAlikeHostsRefusedThroughTheStore(t *testing.T) {
	for _, h := range []string{"GitHub.com", "github.com.", "https://github.com", "github.com:443", "api.github.com", "github.example.com"} {
		t.Run(h, func(t *testing.T) {
			testenv.Isolate(t)
			_, err := profile.Parse([]byte(configWithBlock(h, validBlock)), productionRegistry(t))
			if !errors.Is(err, profile.ErrUnsupportedHost) {
				t.Fatalf("Parse with host %q: error = %v, want ErrUnsupportedHost", h, err)
			}
		})
	}
}

func TestProductionRegistryThroughTheStore(t *testing.T) {
	token := testenv.FakeToken("inline")
	tests := []struct {
		name     string
		config   string
		class    error
		wantText []string
		secret   string
	}{
		{"token as private_key", configWithBlock("github.com", `{"app_id":1,"installation_id":2,"private_key":"`+token+`"}`), profile.ErrInlineSecret, []string{"profiles.claude.github.private_key"}, token},
		{"personal token in a gitlab block", `{"version":1,"profiles":{"gl":{"provider":"gitlab","host":"gitlab.com","gitlab":{"t":"ghp_` + strings.Repeat("a", 36) + `"}}}}`, profile.ErrInlineSecret, []string{"profiles.gl.gitlab.t"}, "ghp_"},
		{"fine-grained token", configWithBlock("github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","note":"github_pat_x"}`), profile.ErrInlineSecret, []string{"profiles.claude.github.note"}, "github_pat_"},
		{"PEM in an unknown member", configWithBlock("github.com", `{"app_id":1,"installation_id":2,"private_key":"file:/k.pem","key":"-----BEGIN RSA PRIVATE KEY-----"}`), profile.ErrInlineSecret, []string{"profiles.claude.github.key"}, "-----BEGIN"},
		{"duplicate app_id", configWithBlock("github.com", `{"app_id":1,"app_id":3,"installation_id":2,"private_key":"file:/k.pem"}`), profile.ErrConfigInvalid, []string{"profiles.claude.github", "app_id"}, ""},
		{"misspelled member", configWithBlock("github.com", `{"app_id":1,"installation_id":2,"privat_key":"file:/k.pem"}`), profile.ErrConfigInvalid, []string{`profiles.claude.github: unknown member "privat_key"`}, ""},
		{"enterprise host", configWithBlock("github.example.com", validBlock), profile.ErrUnsupportedHost, []string{"profiles.claude.host:", "only github.com"}, ""},
		{"keyring", configWithBlock("github.com", `{"app_id":1,"installation_id":2,"private_key":"keyring:claude-github-app"}`), profile.ErrUnsupportedKeyReference, []string{"profiles.claude.github.private_key", "not supported yet"}, "claude-github-app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			_, err := profile.Parse([]byte(tt.config), productionRegistry(t))
			if !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want class %v", err, tt.class)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Errorf("error = %q echoes the secret", err)
			}
		})
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"1", 1, true},
		{"123456", 123456, true},
		{"9007199254740991", 1<<53 - 1, true},
		{"9007199254740992", 0, false},
		{"99999999999999999999", 0, false},
		{"0", 0, false},
		{"0123", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1e6", 0, false},
		{"1.0", 0, false},
		{" 1", 0, false},
		{"1 ", 0, false},
		{"", 0, false},
		{"１", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseID(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseID(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestProfileSettingsRoundTrip(t *testing.T) {
	testenv.Isolate(t)
	settings := ProfileSettings{
		AppID:          123456,
		InstallationID: 789012,
		PrivateKey:     profile.KeyRef{Scheme: profile.SchemeFile, Value: "/keys/claude.pem"},
		Identity:       &BotIdentity{Login: "claude-dev[bot]", UserID: 12345678, ResolvedAt: time.Date(2026, 11, 12, 11, 0, 0, 500, time.FixedZone("CET", 3600))},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"app_id":123456,"installation_id":789012,"private_key":"file:/keys/claude.pem","identity":{"login":"claude-dev[bot]","user_id":12345678,"resolved_at":"2026-11-12T10:00:00Z"}}`
	if string(data) != want {
		t.Errorf("Marshal = %s, want %s", data, want)
	}
	if _, err := profile.Parse([]byte(configWithBlock("github.com", string(data))), productionRegistry(t)); err != nil {
		t.Fatalf("Parse of the encoded block: %v", err)
	}
	decoded, err := DecodeProfileSettings("github.com", data)
	if err != nil {
		t.Fatalf("DecodeProfileSettings: %v", err)
	}
	settings.Identity.ResolvedAt = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if !reflect.DeepEqual(decoded, settings) {
		t.Errorf("decoded = %+v, want %+v", decoded, settings)
	}
	settings.Identity = nil
	data, err = json.Marshal(settings)
	if err != nil {
		t.Fatalf("Marshal without identity: %v", err)
	}
	if strings.Contains(string(data), "identity") {
		t.Errorf("Marshal without identity = %s", data)
	}
}

func TestProfileSettingsMarshalRefusesInvalid(t *testing.T) {
	valid := func() ProfileSettings {
		return ProfileSettings{AppID: 1, InstallationID: 2, PrivateKey: profile.KeyRef{Scheme: profile.SchemeFile, Value: "/k.pem"}}
	}
	tests := []struct {
		name   string
		change func(*ProfileSettings)
	}{
		{"zero App ID", func(s *ProfileSettings) { s.AppID = 0 }},
		{"App ID above 2^53-1", func(s *ProfileSettings) { s.AppID = 1 << 53 }},
		{"no key reference", func(s *ProfileSettings) { s.PrivateKey = profile.KeyRef{} }},
		{"keyring reference", func(s *ProfileSettings) { s.PrivateKey = profile.KeyRef{Scheme: profile.SchemeKeyring, Value: "x"} }},
		{"bad login", func(s *ProfileSettings) {
			s.Identity = &BotIdentity{Login: "dev", UserID: 1, ResolvedAt: time.Now()}
		}},
		{"zero user id", func(s *ProfileSettings) {
			s.Identity = &BotIdentity{Login: "a[bot]", ResolvedAt: time.Now()}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			s := valid()
			tt.change(&s)
			if data, err := json.Marshal(s); err == nil {
				t.Errorf("Marshal = %s, want an error", data)
			}
		})
	}
}

func TestUpdateIdentity(t *testing.T) {
	identity := &BotIdentity{Login: "claude-dev[bot]", UserID: 12345678, ResolvedAt: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
	fresh := &BotIdentity{Login: "other[bot]", UserID: 9, ResolvedAt: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
	prev := ProfileSettings{AppID: 1, InstallationID: 2, Identity: identity}
	tests := []struct {
		name     string
		nextHost string
		next     ProfileSettings
		want     *BotIdentity
	}{
		{"same App keeps the identity", "github.com", ProfileSettings{AppID: 1, InstallationID: 3}, identity},
		{"another App drops it", "github.com", ProfileSettings{AppID: 4, InstallationID: 2}, nil},
		{"another host drops it", "example.com", ProfileSettings{AppID: 1, InstallationID: 2}, nil},
		{"a new identity wins", "github.com", ProfileSettings{AppID: 4, Identity: fresh}, fresh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Update("github.com", prev, tt.nextHost, tt.next)
			if !reflect.DeepEqual(got.Identity, tt.want) {
				t.Errorf("identity = %+v, want %+v", got.Identity, tt.want)
			}
		})
	}
}

func TestProfileRegistration(t *testing.T) {
	testenv.Isolate(t)
	registry := productionRegistry(t)
	if got := registry.Supported(); !reflect.DeepEqual(got, []string{"github"}) {
		t.Errorf("Supported = %v, want [github]", got)
	}
	reg := ProfileRegistration()
	reg.TokenPrefixes[0] = "changed"
	if TokenPrefixes[0] != "ghs_" {
		t.Errorf("ProfileRegistration shares TokenPrefixes with the caller")
	}
}
