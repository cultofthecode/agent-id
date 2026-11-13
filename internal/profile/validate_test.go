package profile

import (
	"errors"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestParseExample(t *testing.T) {
	testenv.Isolate(t)
	config, err := Parse([]byte(exampleConfig), acmeRegistry(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if config.Version != 1 {
		t.Errorf("Version = %d, want 1", config.Version)
	}
	want := map[string]Profile{
		"claude":   {Name: "claude", Provider: "acme", Host: acmeHost, Settings: []byte(`{"id":123456,"key":"file:~/.config/agent-id/keys/claude.pem"}`)},
		"reviewer": {Name: "reviewer", Provider: "acme", Host: acmeHost, Settings: []byte(`{"id":456789,"key":"file:/keys/reviewer.pem"}`)},
		"gl-bot":   {Name: "gl-bot", Provider: "gitlab", Host: "gitlab.example", Settings: []byte(glBotSettings)},
	}
	if len(config.Profiles) != len(want) {
		t.Fatalf("Profiles = %v, want %d profiles", config.Names(), len(want))
	}
	for name, w := range want {
		got := config.Profiles[name]
		if got.Name != w.Name || got.Provider != w.Provider || got.Host != w.Host || string(got.Settings) != string(w.Settings) {
			t.Errorf("Profiles[%q] = %+v, want %+v (settings %s, want %s)", name, got, w, got.Settings, w.Settings)
		}
	}
}

func TestParseProfileNames(t *testing.T) {
	testenv.Isolate(t)
	for _, name := range []string{"a", "a--b", "claude-2", "x" + strings.Repeat("9", 31)} {
		if _, err := Parse([]byte(profileDoc(acmeProfile(name))), acmeRegistry(t)); err != nil {
			t.Errorf("profile name %q: %v", name, err)
		}
	}
	doc := profileDoc(`"claude": {"provider": "acme-", "host": "git.acme.test", "acme-": {}}`)
	if _, err := Parse([]byte(doc), acmeRegistry(t)); err != nil {
		t.Errorf("provider name ending with -: %v, want the provider rule unchanged", err)
	}
}

func TestParseEmptyProfiles(t *testing.T) {
	testenv.Isolate(t)
	config, err := Parse([]byte(profileDoc("")), acmeRegistry(t))
	if err != nil || len(config.Profiles) != 0 {
		t.Fatalf("Parse = %+v, %v; want no profiles", config, err)
	}
}

func TestParseRejects(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name string
		data string
		want error
		msg  string
	}{
		{"empty file", "", ErrConfigInvalid, "not valid JSON at line 1, column 1"},
		{"whitespace only", "  \n ", ErrConfigInvalid, "not valid JSON at line 2"},
		{"trailing comma", "{\n  \"version\": 1,\n}", ErrConfigInvalid, "not valid JSON at line 2"},
		{"comment", "{\n  // a comment\n  \"version\": 1}", ErrConfigInvalid, "not valid JSON at line 2"},
		{"trailing data", profileDoc("") + "\n{}", ErrConfigInvalid, "data after the top-level value"},
		{"trailing garbage", profileDoc("") + " x", ErrConfigInvalid, "not valid JSON"},
		{"truncated", `{"version": 1, "profiles": {`, ErrConfigInvalid, "not valid JSON"},
		{"byte order mark", "\uFEFF" + profileDoc(""), ErrConfigInvalid, "not valid JSON at line 1"},
		{"invalid UTF-8", "{\"version\": 1, \"x\": \"\xff\"}", ErrConfigInvalid, "not valid UTF-8 at line 1, column 22"},
		{"larger than 64 KiB", profileDoc("") + strings.Repeat(" ", MaxSize), ErrConfigInvalid, "larger than 64 KiB"},
		{"nested too deeply", `{"version": 1, "profiles": {}, "x": ` + strings.Repeat("[", 100) + strings.Repeat("]", 100) + `}`, ErrConfigInvalid, "nested deeper than 64 levels"},
		{"array document", `[]`, ErrConfigInvalid, "must be a JSON object"},
		{"string document", `"version"`, ErrConfigInvalid, "must be a JSON object"},
		{"empty object", `{}`, ErrUnsupportedVersion, "version is missing"},
		{"newer version", `{"version": 2, "profiles": {}}`, ErrUnsupportedVersion, "version 2 is newer than this agent-id supports (1); upgrade agent-id"},
		{"huge version", `{"version": 123456789012345678901234567890, "profiles": {}}`, ErrUnsupportedVersion, "upgrade agent-id"},
		{"version string", `{"version": "1", "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version fraction", `{"version": 1.0, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version exponent", `{"version": 1e0, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version zero", `{"version": 0, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version negative", `{"version": -1, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version null", `{"version": null, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"version bool", `{"version": true, "profiles": {}}`, ErrUnsupportedVersion, "version must be the integer 1"},
		{"profiles missing", `{"version": 1}`, ErrConfigInvalid, "profiles is missing"},
		{"profiles array", `{"version": 1, "profiles": []}`, ErrConfigInvalid, "profiles must be an object"},
		{"profiles null", `{"version": 1, "profiles": null}`, ErrConfigInvalid, "profiles must be an object"},
		{"unknown top-level member", `{"version": 1, "profiles": {}, "integrations": {}}`, ErrConfigInvalid, `unknown member "integrations"`},
		{"duplicate top-level member", `{"version": 1, "version": 1, "profiles": {}}`, ErrConfigInvalid, `(top level): duplicate member "version"`},
		{"duplicate profile", profileDoc(acmeProfile("claude") + "," + acmeProfile("claude")), ErrConfigInvalid, `profiles: duplicate member "claude"`},
		{"duplicate in provider block", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "id": 2, "key": "file:/k.pem"}}`), ErrConfigInvalid, `profiles.claude.acme: duplicate member "id"`},
		{"duplicate deep in an unregistered block", profileDoc(`"gl": {"provider": "gitlab", "host": "gitlab.example", "gitlab": {"a": [{"b": 1, "b": 2}]}}`), ErrConfigInvalid, `profiles.gl.gitlab.a[0]: duplicate member "b"`},
		{"duplicate provider member", profileDoc(`"claude": {"provider": "gitlab", "provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrConfigInvalid, `profiles.claude: duplicate member "provider"`},
		{"uppercase profile name", profileDoc(acmeProfile("Claude")), ErrConfigInvalid, `profiles: invalid profile name "Claude": use 1–32 lowercase letters`},
		{"underscore profile name", profileDoc(acmeProfile("my_bot")), ErrConfigInvalid, `invalid profile name "my_bot"`},
		{"empty profile name", profileDoc(acmeProfile("")), ErrConfigInvalid, `invalid profile name ""`},
		{"digit first profile name", profileDoc(acmeProfile("1bot")), ErrConfigInvalid, `invalid profile name "1bot"`},
		{"profile name ending with -", profileDoc(acmeProfile("claude-")), ErrConfigInvalid, `invalid profile name "claude-": use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"`},
		{"profile name starting with -", profileDoc(acmeProfile("-a")), ErrConfigInvalid, `invalid profile name "-a"`},
		{"profile name of 33 characters", profileDoc(acmeProfile("a" + strings.Repeat("b", 32))), ErrConfigInvalid, "invalid profile name"},
		{"profile name with escape", profileDoc(acmeProfile(`a\u001b[31m`)), ErrConfigInvalid, `invalid profile name "a?[31m"`},
		{"profile not an object", profileDoc(`"claude": "acme"`), ErrConfigInvalid, "profiles.claude must be an object"},
		{"provider missing", profileDoc(`"claude": {"host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrConfigInvalid, "profiles.claude.provider is missing"},
		{"provider uppercase", profileDoc(`"claude": {"provider": "Acme", "host": "git.acme.test", "Acme": {}}`), ErrConfigInvalid, "profiles.claude.provider: invalid provider name"},
		{"provider number", profileDoc(`"claude": {"provider": 1, "host": "git.acme.test"}`), ErrConfigInvalid, "profiles.claude.provider: invalid provider name"},
		{"provider empty", profileDoc(`"claude": {"provider": "", "host": "git.acme.test", "": {}}`), ErrConfigInvalid, "profiles.claude.provider: invalid provider name"},
		{"host missing", profileDoc(`"claude": {"provider": "acme", "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrConfigInvalid, "profiles.claude.host is missing"},
		{"host number", profileDoc(`"claude": {"provider": "acme", "host": 443, "acme": {}}`), ErrUnsupportedHost, "profiles.claude.host: must be a string"},
		{"block missing", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "gitlab": {}}`), ErrConfigInvalid, `profiles.claude: provider block "acme" is missing`},
		{"provider typo", profileDoc(`"claude": {"provider": "acmee", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrConfigInvalid, `profiles.claude: provider block "acmee" is missing`},
		{"block not an object", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": []}`), ErrConfigInvalid, "profiles.claude.acme must be an object"},
		{"flat profile", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "id": 1, "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrConfigInvalid, `profiles.claude: unknown member "id"`},
		{"unknown member of an unregistered provider's profile", profileDoc(`"gl": {"provider": "gitlab", "host": "gitlab.example", "gitlab": {}, "extra": 1}`), ErrConfigInvalid, `profiles.gl: unknown member "extra"`},
		{"adapter: unknown member", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem", "kye": "x"}}`), ErrConfigInvalid, "profiles.claude.acme: "},
		{"adapter: string integer", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": "123456", "key": "file:/k.pem"}}`), ErrConfigInvalid, "profiles.claude.acme.id: must be a positive integer"},
		{"adapter: exponent integer", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1e6, "key": "file:/k.pem"}}`), ErrConfigInvalid, "profiles.claude.acme.id: must be a positive integer"},
		{"adapter: host", profileDoc(`"claude": {"provider": "acme", "host": "git.other.test", "acme": {"id": 1, "key": "file:/k.pem"}}`), ErrUnsupportedHost, `profiles.claude.host: "git.other.test" is not supported by the acme provider`},
		{"adapter: keyring", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "keyring:claude-app"}}`), ErrUnsupportedKeyReference, "profiles.claude.acme.key: keyring references are reserved and not supported yet; use file:<path>"},
		{"adapter: relative key", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:keys/claude.pem"}}`), ErrUnsupportedKeyReference, "profiles.claude.acme.key: a file reference needs an absolute path or ~/…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data), acmeRegistry(t))
			checkError(t, err, tt.want, tt.msg)
		})
	}
}

func TestParseRejectsHosts(t *testing.T) {
	testenv.Isolate(t)
	hosts := []string{
		"GIT.acme.test", "git.acme.test.", "https://git.acme.test", "git.acme.test:443", "git.acme.test/org",
		"localhost", " git.acme.test", "git.acme.test ", "git..acme.test", ".acme.test", "-git.acme.test",
		"git-.acme.test", "git_x.acme.test", "user@git.acme.test", "git.acme.test\\u001b[2J", "",
		strings.Repeat("a", 64) + ".test", strings.Repeat("abcdefgh.", 28) + "test",
	}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			doc := profileDoc(`"claude": {"provider": "acme", "host": "` + host + `", "acme": {"id": 1, "key": "file:/k.pem"}}`)
			_, err := Parse([]byte(doc), acmeRegistry(t))
			checkError(t, err, ErrUnsupportedHost, "is not a valid host name (lowercase, no scheme, port or path)")
			if strings.ContainsRune(err.Error(), '\x1b') {
				t.Errorf("error %q echoes a control character", err)
			}
		})
	}
}

func TestParseUnregisteredProvider(t *testing.T) {
	testenv.Isolate(t)
	for name, registry := range map[string]*Registry{"acme registered": acmeRegistry(t), "nil registry": nil} {
		t.Run(name, func(t *testing.T) {
			config, err := Parse([]byte(exampleConfig), registry)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := string(config.Profiles["gl-bot"].Settings); got != glBotSettings {
				t.Errorf("gl-bot settings = %s, want %s", got, glBotSettings)
			}
		})
	}
}

func TestParseValidatorWithoutSentinel(t *testing.T) {
	testenv.Isolate(t)
	registry, err := NewRegistry(Registration{Name: "plain", Validator: plainValidator{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse([]byte(profileDoc(`"p": {"provider": "plain", "host": "plain.test", "plain": {}}`)), registry)
	checkError(t, err, ErrConfigInvalid, "profiles.p.plain: broken")
}

func TestParseLimitsTheClassesOfAValidator(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		class error
		want  error
	}{
		{ErrConfigNotFound, ErrConfigInvalid},
		{ErrProfileNotFound, ErrConfigInvalid},
		{ErrUnsupportedProvider, ErrConfigInvalid},
		{ErrInsecurePermissions, ErrConfigInvalid},
		{ErrUnsupportedVersion, ErrConfigInvalid},
		{ErrUnsupportedHost, ErrUnsupportedHost},
		{ErrUnsupportedKeyReference, ErrUnsupportedKeyReference},
		{ErrInlineSecret, ErrInlineSecret},
	}
	for _, tt := range tests {
		registry, err := NewRegistry(Registration{Name: "class", Validator: classValidator{class: tt.class}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse([]byte(profileDoc(`"p": {"provider": "class", "host": "class.test", "class": {}}`)), registry)
		checkError(t, err, tt.want, "profiles.p.class: key file not found")
		if tt.class != tt.want && errors.Is(err, tt.class) {
			t.Errorf("a validator error of class %v kept its class: %v", tt.class, err)
		}
	}
}

func TestParseKeepsTheStricterClassOfAValidator(t *testing.T) {
	testenv.Isolate(t)
	registry, err := NewRegistry(Registration{Name: "two", Validator: twoClassValidator{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse([]byte(profileDoc(`"p": {"provider": "two", "host": "two.test", "two": {}}`)), registry)
	checkError(t, err, ErrInlineSecret, "profiles.p.two: classes")
}

func TestParseErrorOrder(t *testing.T) {
	testenv.Isolate(t)
	secret := acmePrefix + "S3cr3tV4lue"
	tests := []struct {
		name string
		data string
		want error
		msg  string
	}{
		{"secret before duplicate", `{"version": 1, "version": 1, "profiles": {}, "x": "` + secret + `"}`, ErrInlineSecret, "x: contains"},
		{"secret before unknown member", profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem", "extra": "-----BEGIN RSA PRIVATE KEY-----"}}`), ErrInlineSecret, "profiles.claude.acme.extra: contains"},
		{"secret before version", `{"version": 2, "profiles": {"a": {"provider": "x", "host": "` + secret + `"}}}`, ErrInlineSecret, "profiles.a.host"},
		{"duplicate before version", `{"profiles": {}, "profiles": {}}`, ErrConfigInvalid, "duplicate member"},
		{"version before profiles", `{"version": 2, "profiles": {"Bad": 1}}`, ErrUnsupportedVersion, "newer"},
		{"profiles in document order", profileDoc(`"zeta": {"provider": "acme"}, "alpha": {}`), ErrConfigInvalid, "profiles.zeta.host is missing"},
		{"core member before the adapter", profileDoc(`"claude": {"provider": "acme", "host": "bad host", "acme": {"kye": 1}}`), ErrUnsupportedHost, "profiles.claude.host"},
		{"registered profile error fails the file", profileDoc(`"gl": {"provider": "gitlab", "host": "gitlab.example", "gitlab": {}}, "claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 0, "key": "file:/k.pem"}}`), ErrConfigInvalid, "profiles.claude.acme.id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data), acmeRegistry(t))
			checkError(t, err, tt.want, tt.msg)
		})
	}
}

func TestParseInlineSecrets(t *testing.T) {
	testenv.Isolate(t)
	const marker = "S3cr3tV4lue"
	secrets := map[string]string{
		"registered prefix": acmePrefix + marker,
		"prefix inside":     "file:/keys/" + acmePrefix + marker,
		"PEM":               "-----BEGIN RSA PRIVATE KEY-----" + marker,
		"JWT":               "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxMjMifQ." + marker,
		"newline":           "file:/k.pem\\n" + marker,
		"carriage return":   "file:/k.pem\\r" + marker,
		"NUL":               "file:/k.pem\\u0000" + marker,
	}
	placements := []struct {
		name string
		doc  func(secret string) string
		path string
	}{
		{"core member", func(s string) string {
			return profileDoc(`"claude": {"provider": "acme", "host": "` + s + `", "acme": {"id": 1, "key": "file:/k.pem"}}`)
		}, "profiles.claude.host"},
		{"provider block", func(s string) string {
			return profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "` + s + `"}}`)
		}, "profiles.claude.acme.key"},
		{"unknown member of a provider block", func(s string) string {
			return profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/k.pem", "secret": "` + s + `"}}`)
		}, "profiles.claude.acme.secret"},
		{"unregistered provider's block", func(s string) string {
			return profileDoc(`"gl-bot": {"provider": "gitlab", "host": "gitlab.example", "gitlab": {"deep": [{"v": "` + s + `"}]}}`)
		}, "profiles.gl-bot.gitlab.deep[0].v"},
		{"member name", func(s string) string {
			return profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"` + s + `": 1}}`)
		}, "profiles.claude.acme: contains"},
		{"profile name", func(s string) string {
			return profileDoc(`"` + s + `": {}`)
		}, "profiles: contains"},
		{"top-level member", func(s string) string {
			return `{"version": 1, "profiles": {}, "x": "` + s + `"}`
		}, "x: contains"},
	}
	for kind, secret := range secrets {
		for _, p := range placements {
			t.Run(kind+" in "+p.name, func(t *testing.T) {
				_, err := Parse([]byte(p.doc(secret)), acmeRegistry(t))
				checkError(t, err, ErrInlineSecret, p.path, "remove the value and revoke it if it was real")
				if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), acmePrefix) {
					t.Errorf("error %q echoes the secret", err)
				}
			})
		}
	}
}

func TestLooksSecretJWT(t *testing.T) {
	testenv.Isolate(t)
	const header, payload = "eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxIn0"
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"whole JWT", header + "." + payload + ".c2lnbmF0dXJl", true},
		{"unsecured JWT without signature", "eyJhbGciOiJub25lIn0." + payload + ".", true},
		{"smallest header with alg", "eyJhbGciOiJ4In0." + payload + ".sig", true},
		{"JWT after a space", "Bearer " + header + "." + payload + ".sig", true},
		{"JWT after an equals sign", "https://x.test/?t=" + header + "." + payload + ".sig", true},
		{"JWT after a slash", "file:/keys/" + header + "." + payload + ".sig", true},
		{"JWT after a dot", "x." + header + "." + payload + ".sig", true},
		{"JWT with an empty payload object", header + ".e30.sig", true},
		{"eyJ inside a word", "file:/keys/MoneyJar.prod.pem", false},
		{"eyJ inside a word with a long tail", "file:/keys/MoneyJarAndMoreLetters.production.pem", false},
		{"eyJ at a boundary with a short first segment", "file:/keys/eyJar.prod.pem", false},
		{"eyJ inside a hyphenated word", "file:/keys/key-eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.pem", false},
		{"eyJ inside an underscored word", "file:/keys/x_eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.pem", false},
		{"one-character payload", "file:/keys/" + header + ".x.pem", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksSecret(c.value, nil); got != c.want {
				t.Errorf("looksSecret(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestParseAcceptsAKeyPathThatContainsEyJ(t *testing.T) {
	testenv.Isolate(t)
	doc := profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "file:/keys/MoneyJar.prod.pem"}}`)
	if _, err := Parse([]byte(doc), acmeRegistry(t)); err != nil {
		t.Errorf("Parse with key file:/keys/MoneyJar.prod.pem: %v, want success", err)
	}
}

func TestParseSecretPrefixesComeFromTheRegistry(t *testing.T) {
	testenv.Isolate(t)
	doc := profileDoc(`"gl": {"provider": "gitlab", "host": "gitlab.example", "gitlab": {"v": "` + acmePrefix + `x"}}`)
	if _, err := Parse([]byte(doc), nil); err != nil {
		t.Errorf("Parse without registered prefixes: %v, want success", err)
	}
	_, err := Parse([]byte(doc), acmeRegistry(t))
	checkError(t, err, ErrInlineSecret)
}

func TestSentinelsAreDistinct(t *testing.T) {
	testenv.Isolate(t)
	sentinels := []error{
		ErrConfigNotFound, ErrConfigInvalid, ErrUnsupportedVersion, ErrUnsupportedProvider,
		ErrUnsupportedHost, ErrUnsupportedKeyReference, ErrInlineSecret, ErrInsecurePermissions,
		ErrProfileNotFound,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("%v matches %v", a, b)
			}
		}
	}
	err := Errorf(ErrUnsupportedHost, "host: bad")
	if err.Error() != "host: bad" || !errors.Is(err, ErrUnsupportedHost) || errors.Is(err, ErrConfigInvalid) {
		t.Errorf("Errorf = %q, want only its message and class", err)
	}
}

func TestQuote(t *testing.T) {
	testenv.Isolate(t)
	tests := map[string]string{
		"github.com":               `"github.com"`,
		"a\x1b[31mb":               `"a?[31mb"`,
		"a\u202eb":                 `"a?b"`,
		"a\"b\\c":                  `"a?b?c"`,
		"\xff":                     `"?"`,
		strings.Repeat("x", 70):    `"` + strings.Repeat("x", 64) + `…"`,
		strings.Repeat("ż", 64):    `"` + strings.Repeat("ż", 64) + `"`,
		"line\nbreak\ttab\x00null": `"line?break?tab?null"`,
	}
	for in, want := range tests {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
