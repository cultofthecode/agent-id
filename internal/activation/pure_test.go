package activation

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestReadContextAndDecide(t *testing.T) {
	testenv.Isolate(t)
	valid := []string{"AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=3"}
	with := func(over ...string) []string { return append(slices.Clone(valid), over...) }
	tests := []struct {
		name    string
		env     []string
		profile string
		wantErr error
		want    string
	}{
		{"no context", []string{"PATH=/bin"}, "claude", nil, ""},
		{"stray variables only", []string{"AGENT_ID_ACTIVE_PROFILE=claude"}, "other", nil, ""},
		{"same profile", valid, "claude", nil, "reenter"},
		{"different profile", valid, "reviewer", ErrNestedProfile, `"claude"`},
		{"version 2", with("AGENT_ID_CONTEXT=2"), "claude", ErrContextInvalid, "incompatible agent-id"},
		{"empty version", with("AGENT_ID_CONTEXT="), "claude", ErrContextInvalid, "incompatible"},
		{"version with space", with("AGENT_ID_CONTEXT=1 "), "claude", ErrContextInvalid, "incompatible"},
		{"profile missing", slices.DeleteFunc(slices.Clone(valid), func(s string) bool { return strings.HasPrefix(s, "AGENT_ID_ACTIVE") }), "claude", ErrContextInvalid, "AGENT_ID_ACTIVE_PROFILE is missing"},
		{"profile uppercase", with("AGENT_ID_ACTIVE_PROFILE=Claude"), "claude", ErrContextInvalid, "not a valid profile name"},
		{"profile metacharacters", with("AGENT_ID_ACTIVE_PROFILE=a;b"), "claude", ErrContextInvalid, "not a valid profile name"},
		{"profile too long", with("AGENT_ID_ACTIVE_PROFILE=" + strings.Repeat("a", 33)), "claude", ErrContextInvalid, "not a valid profile name"},
		{"base not a number", with("AGENT_ID_GIT_CONFIG_BASE=x"), "claude", ErrContextInvalid, "AGENT_ID_GIT_CONFIG_BASE"},
		{"base negative", with("AGENT_ID_GIT_CONFIG_BASE=-1"), "claude", ErrContextInvalid, "AGENT_ID_GIT_CONFIG_BASE"},
		{"base above end", with("AGENT_ID_GIT_CONFIG_BASE=4"), "claude", ErrContextInvalid, "greater than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := ReadContext(tt.env)
			if err == nil {
				var start Start
				start, err = Decide(ctx, tt.profile)
				if err == nil && (start == StartReenter) != (tt.want == "reenter") {
					t.Errorf("start = %v", start)
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && !strings.Contains(err.Error(), tt.want) {
				t.Errorf("message %q lacks %q", err, tt.want)
			}
		})
	}
}

func TestCheckInheritedGit(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"nothing", nil, ""},
		{"empty count", []string{"GIT_CONFIG_COUNT="}, ""},
		{"valid", []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_VALUE_0="}, ""},
		{"missing key", []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_VALUE_0=x", "GIT_CONFIG_VALUE_1=y"}, "GIT_CONFIG_KEY_1"},
		{"missing value", []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=a.b"}, "GIT_CONFIG_VALUE_0"},
		{"letters", []string{"GIT_CONFIG_COUNT=x"}, "GIT_CONFIG_COUNT"},
		{"negative", []string{"GIT_CONFIG_COUNT=-1"}, "GIT_CONFIG_COUNT"},
		{"too large", []string{"GIT_CONFIG_COUNT=1001"}, "GIT_CONFIG_COUNT"},
		{"trailing space", []string{"GIT_CONFIG_COUNT=2 "}, "GIT_CONFIG_COUNT"},
		{"hex", []string{"GIT_CONFIG_COUNT=0x2"}, "GIT_CONFIG_COUNT"},
		{"credential helper", []string{"GIT_CONFIG_PARAMETERS='credential.helper'='store'"}, "credential.helper"},
		{"mixed case url", []string{"GIT_CONFIG_PARAMETERS='URL.x.insteadOf'='y'"}, "url.<subsection>.insteadof"},
		{"http", []string{"GIT_CONFIG_PARAMETERS='http.extraheader'='A: b'"}, "http.extraheader"},
		{"include path", []string{"GIT_CONFIG_PARAMETERS='include.path'='/tmp/x.gitconfig'"}, "include.path"},
		{"includeIf", []string{"GIT_CONFIG_PARAMETERS='includeIf.gitdir:/x/.path'='/y'"}, "includeif.<subsection>.path"},
		{"push gpgSign", []string{"GIT_CONFIG_PARAMETERS='push.gpgSign'='true'"}, "push.gpgsign"},
		{"old quoting", []string{"GIT_CONFIG_PARAMETERS='credential.helper=store'"}, "credential.helper"},
		{"bare key", []string{"GIT_CONFIG_PARAMETERS='http.sslverify'"}, "http.sslverify"},
		{"harmless", []string{"GIT_CONFIG_PARAMETERS='core.pager'='less'"}, ""},
		{"escape kept", []string{`GIT_CONFIG_PARAMETERS='alias.x'='!cmd'\'''`}, ""},
		{"unterminated", []string{"GIT_CONFIG_PARAMETERS='core.pager"}, "cannot be parsed"},
		{"url credentials not echoed", []string{"GIT_CONFIG_PARAMETERS='credential.https://user:tok@github.com.helper'='x'"}, "credential.<subsection>.helper"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckInheritedGit(tt.env)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("error %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInheritedGitConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
			for _, leak := range []string{"user", "tok", "store", "less"} {
				if strings.Contains(strings.ReplaceAll(err.Error(), "credential", ""), leak) && tt.name == "url credentials not echoed" {
					t.Errorf("message leaks %q", leak)
				}
			}
		})
	}
}

func TestParseGitConfigParameters(t *testing.T) {
	keys, err := ParseGitConfigParameters(`'a.b'='c' 'd.e=f' 'g.h' 'i.j'='it'\''s'`)
	if err != nil || !slices.Equal(keys, []string{"a.b", "d.e", "g.h", "i.j"}) {
		t.Errorf("keys %v, err %v", keys, err)
	}
}

func testPlan() Plan {
	return Plan{
		Profile: "claude", Mechanism: MechanismExec, Binary: "/usr/local/bin/agent-id",
		Drop:          []string{"GH_TOKEN", "GITHUB_TOKEN", "GIT_ASKPASS"},
		Set:           map[string]string{"GH_TOKEN": "agent-id-no-token", "GIT_TERMINAL_PROMPT": "0"},
		GitConfig:     []GitConfigEntry{{"credential.helper", ""}, {"core.askPass", ""}},
		PathPrepend:   []string{"/cache/neutral-bin"},
		TokenPrefixes: []string{testenv.FakeTokenPrefix},
	}
}

func TestBuild(t *testing.T) {
	testenv.Isolate(t)
	parent := []string{
		"PATH=/usr/bin:/cache/neutral-bin:/bin:/cache/neutral-bin", "GH_TOKEN=" + testenv.FakeToken("personal"), "GITHUB_TOKEN=" + testenv.FakeToken("p2"),
		"GIT_ASKPASS=/x", "AGENT_ID_STRAY=1", "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_VALUE_0=1", "GIT_CONFIG_KEY_1=c.d", "GIT_CONFIG_VALUE_1=2",
		"GIT_CONFIG_KEY_5=stale", "HOMEBREW_GITHUB_API_TOKEN=keep",
	}
	out, err := Build(parent, testPlan())
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnv(out)
	want := map[string]string{
		"GH_TOKEN": "agent-id-no-token", "GIT_TERMINAL_PROMPT": "0", "GIT_CONFIG_COUNT": "4",
		"GIT_CONFIG_KEY_0": "a.b", "GIT_CONFIG_VALUE_1": "2", "GIT_CONFIG_KEY_2": "credential.helper", "GIT_CONFIG_VALUE_2": "",
		"GIT_CONFIG_KEY_3": "core.askPass", "GIT_CONFIG_KEY_5": "stale", "AGENT_ID_GIT_CONFIG_BASE": "2", "AGENT_ID_GIT_CONFIG_END": "4",
		"AGENT_ID_CONTEXT": "1", "AGENT_ID_ACTIVE_PROFILE": "claude", "PATH": "/cache/neutral-bin:/usr/bin:/bin", "HOMEBREW_GITHUB_API_TOKEN": "keep",
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("%s = %q (set %v), want %q", k, got, ok, v)
		}
	}
	for _, gone := range []string{"GITHUB_TOKEN", "GIT_ASKPASS", "AGENT_ID_STRAY"} {
		if _, ok := env[gone]; ok {
			t.Errorf("%s survived", gone)
		}
	}
	if !slices.IsSorted(out) {
		t.Error("output is not sorted")
	}
}

func TestBuildIsIdempotentAndAppendOnly(t *testing.T) {
	testenv.Isolate(t)
	parents := [][]string{
		nil,
		{"PATH="},
		{"PATH=/a:/cache/neutral-bin", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=x.y", "GIT_CONFIG_VALUE_0=z"},
		{"GIT_CONFIG_COUNT=", "GH_TOKEN=x"},
	}
	plans := []Plan{testPlan(), func() Plan { p := testPlan(); p.GitConfig = nil; return p }()}
	for _, parent := range parents {
		for _, plan := range plans {
			once, err := Build(parent, plan)
			if err != nil {
				t.Fatal(err)
			}
			twice, err := Build(once, plan)
			if err != nil {
				t.Fatalf("re-entry: %v", err)
			}
			if !slices.Equal(once, twice) {
				t.Errorf("not idempotent:\n%v\n%v", once, twice)
			}
			if p := parseEnv(parent); p["GIT_CONFIG_COUNT"] == "1" {
				if got := parseEnv(once); got["GIT_CONFIG_KEY_0"] != "x.y" || got["GIT_CONFIG_VALUE_0"] != "z" {
					t.Error("an inherited entry changed")
				}
			}
		}
	}
}

func TestBuildReentryGuards(t *testing.T) {
	testenv.Isolate(t)
	once, err := Build(nil, testPlan())
	if err != nil {
		t.Fatal(err)
	}
	unset := slices.DeleteFunc(slices.Clone(once), func(s string) bool { return strings.HasPrefix(s, "GIT_CONFIG_COUNT=") })
	appended := append(slices.Clone(once), "GIT_CONFIG_COUNT=3")
	for name, env := range map[string]([]string){"count unset": unset, "another tool appended": appended} {
		if _, err := Build(env, testPlan()); !errors.Is(err, ErrContextInvalid) {
			t.Errorf("%s: error %v, want ErrContextInvalid", name, err)
		}
	}
	other := testPlan()
	other.Profile = "reviewer"
	if _, err := Build(once, other); !errors.Is(err, ErrNestedProfile) {
		t.Errorf("different profile: %v", err)
	}
}

func TestBuildRefusesUnsafePlans(t *testing.T) {
	testenv.Isolate(t)
	token := testenv.FakeToken("x")
	mutations := map[string]func(*Plan){
		"empty drop":       func(p *Plan) { p.Drop = nil },
		"token in Set":     func(p *Plan) { p.Set["X"] = token },
		"PEM in git value": func(p *Plan) { p.GitConfig[0].Value = "-----BEGIN PRIVATE KEY-----" },
		"JWT in Set":       func(p *Plan) { p.Set["X"] = "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxIn0.c2ln" },
		"newline in Set":   func(p *Plan) { p.Set["X"] = "a\nb" },
		"NUL in path":      func(p *Plan) { p.PathPrepend = []string{"/a\x00b"} },
		"relative path":    func(p *Plan) { p.PathPrepend = []string{"rel"} },
		"colon in path":    func(p *Plan) { p.PathPrepend = []string{"/a:/b"} },
		"bad name":         func(p *Plan) { p.Set["A=B"] = "x" },
		"owned name":       func(p *Plan) { p.Set["AGENT_ID_X"] = "x" },
		"bad mechanism":    func(p *Plan) { p.Mechanism = "shell" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := testPlan()
			p.Set = map[string]string{"GH_TOKEN": "d"}
			mutate(&p)
			_, err := Build(nil, p)
			if !errors.Is(err, ErrPlanUnsafe) {
				t.Fatalf("error %v, want ErrPlanUnsafe", err)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "BEGIN") {
				t.Error("the error echoes the value")
			}
		})
	}
}

func TestClassifyRemoteURL(t *testing.T) {
	hosts := []string{"github.com", "ssh.github.com"}
	tests := []struct {
		url      string
		kind     URLKind
		provider bool
	}{
		{"git@github.com:o/r.git", KindSCP, true},
		{"github.com:o/r.git", KindSCP, true},
		{"ssh://git@github.com/o/r.git", KindSSH, true},
		{"git+ssh://git@github.com/o/r.git", KindSSH, true},
		{"ssh+git://github.com/o/r.git", KindSSH, true},
		{"ssh://git@SSH.GitHub.com:443/o/r.git", KindSSH, true},
		{"GIT@GITHUB.COM:o/r.git", KindSCP, true},
		{"https://github.com/o/r.git", KindHTTP, true},
		{"https://u:p@github.com/o/r.git", KindHTTP, true},
		{"git@github.com.evil.example:o/r.git", KindSCP, false},
		{"git@evilgithub.com:o/r.git", KindSCP, false},
		{"https://github.com.evil.example/o/r.git", KindHTTP, false},
		{"file:///srv/r.git", KindOther, false},
		{"/srv/github.com:x/r.git", KindOther, false},
		{"../relative", KindOther, false},
	}
	for _, tt := range tests {
		got := ClassifyRemoteURL(tt.url, hosts)
		if got.Kind != tt.kind || got.Provider != tt.provider {
			t.Errorf("%s: %+v, want kind %v provider %v", tt.url, got, tt.kind, tt.provider)
		}
	}
}

func TestHasPasswordAndRedact(t *testing.T) {
	for url, want := range map[string]bool{
		"https://github.com/o/r": false, "https://user@github.com/o/r": false, "https://u:p@github.com/o/r": true,
		"https://u:p%40x@github.com/o/r": true, "https://u%40x@github.com/o/r": false,
	} {
		if got := HasPassword(url); got != want {
			t.Errorf("HasPassword(%s) = %v", url, got)
		}
	}
	if got := Redact("https://u:secret@github.com/o/r.git"); strings.Contains(got, "secret") || !strings.Contains(got, "[redacted]@github.com") {
		t.Errorf("Redact = %q", got)
	}
}

func entries(kv ...string) []ConfigEntry {
	var out []ConfigEntry
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, ConfigEntry{Scope: "global", Origin: "file:/h/.gitconfig", Key: kv[i], Value: kv[i+1]})
	}
	return out
}

func TestRewritesToSSH(t *testing.T) {
	base := "https://github.com/"
	tests := []struct {
		name string
		e    []ConfigEntry
		want bool
	}{
		{"none", nil, false},
		{"https to ssh", entries("url.git@github.com:.insteadof", "https://github.com/"), true},
		{"ssh:// replacement", entries("url.ssh://git@github.com/.insteadof", "https://github.com/"), true},
		{"scoped to a repo under base", entries("url.git@github.com:o/.insteadof", "https://github.com/o/"), true},
		{"unrelated host", entries("url.git@gitlab.com:.insteadof", "https://gitlab.com/"), false},
		{"https to https", entries("url.https://mirror.example/.insteadof", "https://github.com/"), false},
		{"longer https rule wins for the base", entries("url.git@github.com:.insteadof", "https://github.com/", "url.https://x.example/.insteadof", "https://github.com/"), false},
		{"pushInsteadOf ssh", entries("url.git@github.com:.pushinsteadof", "https://github.com/"), true},
		{"pushInsteadOf https shadows insteadOf ssh for push only", entries("url.git@github.com:.insteadof", "https://github.com/", "url.https://m.example/.pushinsteadof", "https://github.com/"), true},
		{"upper-case key", entries("URL.git@github.com:.INSTEADOF", "HTTPS://GITHUB.COM/"), true},
	}
	for _, tt := range tests {
		if _, got := RewritesToSSH(tt.e, base); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEffectiveHeaders(t *testing.T) {
	u := "https://github.com/o/r.git"
	tests := []struct {
		name string
		e    []ConfigEntry
		want []string
	}{
		{"none", nil, nil},
		{"generic", entries("http.extraheader", "Authorization: Basic x"), []string{"Authorization"}},
		{"reset after generic", entries("http.extraheader", "Authorization: Basic x", "http.https://github.com/.extraheader", ""), nil},
		{"host scoped then reset", entries("http.https://github.com/.extraheader", "Cookie: a=b", "http.https://github.com/.extraheader", ""), nil},
		{"path scoped survives the host reset", entries("http.https://github.com/o/r.git.extraheader", "Authorization: Basic x", "http.https://github.com/.extraheader", ""), []string{"Authorization"}},
		{"path scoped with a harmless second header", entries("http.https://github.com/o/r.git.extraheader", "Authorization: Basic x", "http.https://github.com/o/r.git.extraheader", "X-Ok: 1"), []string{"Authorization", "X-Ok"}},
		{"cookie after reset", entries("http.https://github.com/.extraheader", "", "http.https://github.com/.extraheader", "Cookie: a=b"), []string{"Cookie"}},
		{"other host", entries("http.https://gitlab.com/.extraheader", "Authorization: x"), nil},
		{"other scheme", entries("http.http://github.com/.extraheader", "Authorization: x"), nil},
		{"other path", entries("http.https://github.com/x/y.git.extraheader", "Authorization: x"), nil},
	}
	for _, tt := range tests {
		if got := EffectiveHeaders(tt.e, u); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNetrcHosts(t *testing.T) {
	machines, def, err := NetrcHosts("machine github.com login x password y\n# note\nmacdef init\n machine evil.example\n\ndefault login a password b\nmachine two\n  login q\n")
	if err != nil || !slices.Equal(machines, []string{"github.com", "two"}) || !def {
		t.Errorf("machines %v default %v err %v", machines, def, err)
	}
	if _, _, err := NetrcHosts("garbage here"); err == nil {
		t.Error("an unknown keyword was accepted")
	}
}

func TestParseConfigList(t *testing.T) {
	got := ParseConfigList([]byte("global\x00file:/h/.gitconfig\x00url.x.insteadof\nhttps://a/\x00command\x00command line:\x00credential.helper\n\x00"))
	if len(got) != 2 || got[0].Key != "url.x.insteadof" || got[0].Value != "https://a/" || got[1].Key != "credential.helper" || got[1].Value != "" || got[1].Scope != "command" {
		t.Errorf("entries %+v", got)
	}
}

func TestReviewRegressions(t *testing.T) {
	testenv.Isolate(t)
	u := "https://github.com/o/r.git"
	t.Run("core.askPass in GIT_CONFIG_PARAMETERS is refused", func(t *testing.T) {
		if err := CheckInheritedGit([]string{"GIT_CONFIG_PARAMETERS='core.askPass'='/x'"}); !errors.Is(err, ErrInheritedGitConfig) {
			t.Errorf("error %v", err)
		}
	})
	t.Run("a generic reset does not clear a more specific header", func(t *testing.T) {
		e := entries("http.https://github.com/o/r.git.extraheader", "Authorization: Basic x", "http.extraheader", "")
		if got := EffectiveHeaders(e, u); !slices.Equal(got, []string{"Authorization"}) {
			t.Errorf("headers %v", got)
		}
	})
	t.Run("a header without a colon is never echoed", func(t *testing.T) {
		e := entries("http.https://github.com/.extraheader", "Bearer FAKE-SECRET-VALUE-0123456789")
		got := EffectiveHeaders(e, u)
		if len(got) != 1 || strings.Contains(got[0], "FAKE") || got[0] != "unnamed header" {
			t.Errorf("headers %v", got)
		}
	})
	t.Run("a header scoped under the host is found", func(t *testing.T) {
		e := entries("http.https://github.com/acme/.extraheader", "Authorization: Basic x", "http.https://github.com/.extraheader", "")
		if got := ScopedHeaders(e, []string{"github.com"}); !slices.Equal(got, []string{"Authorization"}) {
			t.Errorf("headers %v", got)
		}
		if got := ScopedHeaders(entries("http.https://gitlab.com/a/.extraheader", "A: b"), []string{"github.com"}); len(got) != 0 {
			t.Errorf("another host: %v", got)
		}
	})
	t.Run("a trailing dot does not hide the host", func(t *testing.T) {
		for _, raw := range []string{"git@github.com.:o/r.git", "ssh://git@github.com./o/r.git", "https://github.com./o/r.git"} {
			if !ClassifyRemoteURL(raw, []string{"github.com"}).Provider {
				t.Errorf("%s is not classified as the provider's", raw)
			}
		}
	})
	t.Run("a comment mark inside a netrc line is not a comment", func(t *testing.T) {
		machines, _, err := NetrcHosts("login x # machine github.com\n")
		if err == nil && len(machines) == 0 {
			t.Errorf("machines %v", machines)
		}
	})
}
