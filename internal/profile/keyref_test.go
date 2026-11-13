package profile

import (
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestParseKeyRef(t *testing.T) {
	testenv.Isolate(t)
	accepted := map[string]KeyRef{
		"file:/abs/path.pem":                      {Scheme: SchemeFile, Value: "/abs/path.pem"},
		"file:/abs//x/../path.pem":                {Scheme: SchemeFile, Value: "/abs/path.pem"},
		"file:/":                                  {Scheme: SchemeFile, Value: "/"},
		"file:~/path.pem":                         {Scheme: SchemeFile, Value: "~/path.pem"},
		"file:~/a/./b/../c.pem":                   {Scheme: SchemeFile, Value: "~/a/c.pem"},
		"file:~/.config/agent-id/keys/claude.pem": {Scheme: SchemeFile, Value: "~/.config/agent-id/keys/claude.pem"},
	}
	for in, want := range accepted {
		got, err := ParseKeyRef(in)
		if err != nil || got != want {
			t.Errorf("ParseKeyRef(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	rejected := []struct {
		in  string
		msg string
	}{
		{"file:relative/path.pem", "a file reference needs an absolute path or ~/…"},
		{"file:./x.pem", "a file reference needs an absolute path or ~/…"},
		{"file:", "a file reference needs an absolute path or ~/…"},
		{"file:~", "a file reference needs an absolute path or ~/…"},
		{"file:~/", "a file reference needs an absolute path or ~/…"},
		{"file:~/..", "a file reference needs an absolute path or ~/…"},
		{"file:~/../other/x.pem", "a file reference needs an absolute path or ~/…"},
		{"file:~/a/../../x.pem", "a file reference needs an absolute path or ~/…"},
		{"file:~user/x.pem", "a file reference needs an absolute path or ~/…"},
		{"keyring:claude-github-app", "keyring references are reserved and not supported yet; use file:<path>"},
		{"keyring:", "keyring references are reserved"},
		{"/abs/path.pem", "unknown key reference; use file:<path>"},
		{"", "unknown key reference; use file:<path>"},
		{"ssh:key", "unknown key reference"},
		{"FILE:/abs.pem", "unknown key reference"},
		{"file:/abs\n.pem", "unknown key reference"},
		{"file:/abs\x00.pem", "unknown key reference"},
	}
	for _, tt := range rejected {
		_, err := ParseKeyRef(tt.in)
		checkError(t, err, ErrUnsupportedKeyReference, tt.msg)
		if tt.in != "" && strings.Contains(err.Error(), tt.in) {
			t.Errorf("ParseKeyRef(%q) error %q echoes the value", tt.in, err)
		}
	}
}

func TestKeyRefPath(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		ref  KeyRef
		home string
		want string
	}{
		{KeyRef{Scheme: SchemeFile, Value: "/abs/k.pem"}, "", "/abs/k.pem"},
		{KeyRef{Scheme: SchemeFile, Value: "~/keys/k.pem"}, "/home/dev", "/home/dev/keys/k.pem"},
	}
	for _, tt := range tests {
		got, err := tt.ref.Path(tt.home)
		if err != nil || got != tt.want {
			t.Errorf("%v.Path(%q) = %q, %v; want %q", tt.ref, tt.home, got, err, tt.want)
		}
	}
	_, err := KeyRef{Scheme: SchemeFile, Value: "~/k.pem"}.Path("relative")
	checkError(t, err, ErrUnsupportedKeyReference, "HOME is not an absolute path")
	_, err = KeyRef{Scheme: SchemeKeyring, Value: "x"}.Path("/home/dev")
	checkError(t, err, ErrUnsupportedKeyReference, "have no file path")
	if got := (KeyRef{Scheme: SchemeFile, Value: "~/k.pem"}).String(); got != "file:~/k.pem" {
		t.Errorf("String() = %q", got)
	}
}

func TestPath(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"XDG set", map[string]string{"XDG_CONFIG_HOME": "/tmp/x", "HOME": "/home/dev"}, "/tmp/x/agent-id/config.json"},
		{"XDG unset", map[string]string{"HOME": "/home/dev"}, "/home/dev/.config/agent-id/config.json"},
		{"XDG empty", map[string]string{"XDG_CONFIG_HOME": "", "HOME": "/home/dev"}, "/home/dev/.config/agent-id/config.json"},
		{"XDG relative", map[string]string{"XDG_CONFIG_HOME": "rel", "HOME": "/home/dev"}, "/home/dev/.config/agent-id/config.json"},
		{"macOS home", map[string]string{"HOME": "/Users/dev"}, "/Users/dev/.config/agent-id/config.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Path(func(k string) string { return tt.env[k] })
			if err != nil || got != tt.want {
				t.Errorf("Path = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	for _, env := range []map[string]string{{}, {"HOME": "rel"}, {"XDG_CONFIG_HOME": "rel", "HOME": ""}} {
		_, err := Path(func(k string) string { return env[k] })
		checkError(t, err, ErrConfigNotFound, "neither XDG_CONFIG_HOME nor HOME is an absolute path")
	}
}

func TestPathUsesTheIsolatedEnvironment(t *testing.T) {
	env := testenv.Isolate(t)
	if got, want := configPath(t), env.ConfigHome+"/agent-id/config.json"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestNewRegistry(t *testing.T) {
	testenv.Isolate(t)
	bad := []struct {
		name string
		regs []Registration
		msg  string
	}{
		{"invalid name", []Registration{{Name: "Acme", Validator: acmeValidator{}}}, "invalid provider name"},
		{"empty name", []Registration{{Validator: acmeValidator{}}}, "invalid provider name"},
		{"nil validator", []Registration{{Name: "acme"}}, "has no settings validator"},
		{"duplicate", []Registration{{Name: "acme", Validator: acmeValidator{}}, {Name: "acme", Validator: acmeValidator{}}}, "registered twice"},
		{"core member name host", []Registration{{Name: "host", Validator: acmeValidator{}}}, "invalid provider name"},
		{"core member name provider", []Registration{{Name: "provider", Validator: acmeValidator{}}}, "invalid provider name"},
		{"short prefix", []Registration{{Name: "acme", Validator: acmeValidator{}, TokenPrefixes: []string{"a_"}}}, "invalid token prefix"},
		{"prefix with space", []Registration{{Name: "acme", Validator: acmeValidator{}, TokenPrefixes: []string{"ab c"}}}, "invalid token prefix"},
	}
	for _, tt := range bad {
		if _, err := NewRegistry(tt.regs...); err == nil || !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("%s: NewRegistry error = %v, want %q", tt.name, err, tt.msg)
		}
	}
	prefixes := []string{"zz_"}
	registry, err := NewRegistry(Registration{Name: "zeta", Validator: acmeValidator{}, TokenPrefixes: prefixes}, Registration{Name: "acme", Validator: acmeValidator{}})
	if err != nil {
		t.Fatal(err)
	}
	prefixes[0] = "changed"
	if got := registry.Supported(); !slices.Equal(got, []string{"acme", "zeta"}) {
		t.Errorf("Supported = %v", got)
	}
	if got := registry.TokenPrefixes(); !slices.Equal(got, []string{"zz_"}) {
		t.Errorf("TokenPrefixes = %v, want a copy of the registered prefixes", got)
	}
	got := registry.TokenPrefixes()
	got[0] = "changed"
	if again := registry.TokenPrefixes(); !slices.Equal(again, []string{"zz_"}) {
		t.Errorf("TokenPrefixes after the caller changed its result = %v, want the registered prefixes", again)
	}
	if !registry.Supports("acme") || registry.Supports("gitlab") {
		t.Error("Supports does not match the registrations")
	}
	var none *Registry
	if none.Supports("acme") || none.Supported() != nil || none.TokenPrefixes() != nil {
		t.Error("a nil registry supports a provider")
	}
}
