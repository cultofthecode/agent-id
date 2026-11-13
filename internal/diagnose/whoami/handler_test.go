package whoami

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type resolver struct {
	profile profile.Profile
	err     error
	calls   *int
}

func (r resolver) Resolve(string) (profile.Profile, error) {
	*r.calls++
	return r.profile, r.err
}

type tokens struct {
	token provider.Token
	err   error
	calls int
}

func (t *tokens) Token(_ context.Context, _ token.Source) (provider.Token, error) {
	t.calls++
	return t.token, t.err
}

type rig struct {
	h        *Handler
	fake     *providertest.Fake
	tokens   *tokens
	resolves int
	opens    int
}

func newRig(t *testing.T, c Context) *rig {
	t.Helper()
	testenv.Isolate(t)
	r := &rig{fake: providertest.NewFake(), tokens: &tokens{}}
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	r.tokens.token = provider.Token{Value: testenv.FakeToken("whoami"), ExpiresAt: now.Add(42*time.Minute + 30*time.Second)}
	r.fake.SetIdentityCheck(provider.IdentityCheck{Bot: providertest.FakeBot, Cached: providertest.FakeBot, Match: true})
	r.h = &Handler{
		ReadContext: func() Context { return c },
		Profiles: func() (ProfileResolver, error) {
			return resolver{profile: profile.Profile{Name: "claude", Provider: providertest.FakeName, Host: providertest.FakeHost}, calls: &r.resolves}, nil
		},
		OpenProvider: func(profile.Profile) (provider.Provider, error) { r.opens++; return r.fake, nil },
		Tokens:       r.tokens,
		Output:       diagnose.Output{Patterns: diagnose.NewPatterns([]string{"ghs_", testenv.FakeTokenPrefix})},
		Now:          func() time.Time { return now },
	}
	return r
}

var execContext = Context{Present: true, Profile: "claude", Mechanism: "exec"}

func (r *rig) run(t *testing.T, asJSON bool) (string, int) {
	t.Helper()
	out, code, err := r.h.Handle(context.Background(), Input{JSON: asJSON})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if strings.Contains(string(out), testenv.FakeTokenPrefix) || r.tokens.token.Value != "" && strings.Contains(string(out), r.tokens.token.Value) {
		t.Fatalf("output holds a token: %s", out)
	}
	return string(out), code
}

func TestOutsideAContextTouchesNothing(t *testing.T) {
	r := newRig(t, Context{})
	for _, asJSON := range []bool{false, true} {
		out, code := r.run(t, asJSON)
		want := "Context:       personal/unmanaged\n"
		if asJSON {
			want = `{"context":"personal/unmanaged","schema":1}` + "\n"
		}
		if out != want || code != 0 {
			t.Errorf("json=%v: output %q exit %d, want %q exit 0", asJSON, out, code, want)
		}
	}
	if r.resolves != 0 || r.opens != 0 || r.tokens.calls != 0 {
		t.Errorf("outside a context: %d resolves, %d provider opens, %d token calls; want none", r.resolves, r.opens, r.tokens.calls)
	}
}

func TestExecContext(t *testing.T) {
	r := newRig(t, execContext)
	out, code := r.run(t, false)
	want := "Context:       agent\nProfile:       claude\nProvider:      fake (git.agent-id.test)\nIdentity:      fake-bot[bot]\nInstallation:  fake-installation\nCredentials:   fake installation token\nToken status:  valid (expires in 42m)\nActivation:    exec\n"
	if out != want || code != 0 {
		t.Errorf("output %q exit %d, want %q exit 0", out, code, want)
	}
	js, _ := r.run(t, true)
	if !strings.Contains(js, `"expires_at":"2026-11-13T10:42:30Z"`) || !strings.Contains(js, `"activation":"exec"`) {
		t.Errorf("JSON = %s", js)
	}
}

func TestEnvContext(t *testing.T) {
	r := newRig(t, Context{Present: true, Profile: "claude", Mechanism: "env"})
	if out, code := r.run(t, false); !strings.HasSuffix(out, "Activation:    env\n") || code != 0 {
		t.Errorf("output %q exit %d", out, code)
	}
}

func TestIdentityNotResolved(t *testing.T) {
	r := newRig(t, execContext)
	r.fake.SetIdentityCheck(provider.IdentityCheck{})
	out, _ := r.run(t, false)
	if !strings.Contains(out, "Identity:      not resolved (run agent-id doctor --profile claude)\n") {
		t.Errorf("output %q", out)
	}
	js, _ := r.run(t, true)
	if !strings.Contains(js, `"identity":null`) {
		t.Errorf("JSON = %s", js)
	}
}

func TestDamagedAndIncompatibleContexts(t *testing.T) {
	for _, problem := range []string{"damaged: AGENT_ID_ACTIVE_PROFILE is missing", "incompatible context version 2"} {
		r := newRig(t, Context{Present: true, Problem: problem})
		out, code := r.run(t, false)
		want := "Context:       agent (" + problem + ")\nToken status:  not checked (damaged context)\n"
		if out != want || code != 1 {
			t.Errorf("%s: output %q exit %d, want %q exit 1", problem, out, code, want)
		}
		js, _ := r.run(t, true)
		if !strings.Contains(js, `"context_problem":"`+problem+`"`) || !strings.Contains(js, `"profile":null`) {
			t.Errorf("%s: JSON = %s", problem, js)
		}
		if r.resolves != 0 || r.tokens.calls != 0 {
			t.Errorf("%s: a damaged context must not resolve or mint", problem)
		}
	}
}

func TestProfileNotFound(t *testing.T) {
	r := newRig(t, execContext)
	r.h.Profiles = func() (ProfileResolver, error) {
		return resolver{err: profile.Errorf(profile.ErrProfileNotFound, "profile claude not found"), calls: &r.resolves}, nil
	}
	out, code := r.run(t, false)
	if !strings.Contains(out, "Profile:       claude (not found in the configuration)\n") || !strings.Contains(out, "Token status:  not checked (profile not found)\n") || code != 1 {
		t.Errorf("output %q exit %d", out, code)
	}
	js, _ := r.run(t, true)
	if !strings.Contains(js, `"profile":"claude"`) || !strings.Contains(js, `"reason":"profile not found"`) {
		t.Errorf("JSON = %s", js)
	}
}

func TestTokenFailureClasses(t *testing.T) {
	tests := []struct {
		err   error
		class string
		hint  string
	}{
		{provider.ErrUnauthorized, "unauthorized", "the host rejected"},
		{provider.ErrForbidden, "forbidden", "the host denied access"},
		{provider.ErrNotFound, "not found", "the installation does not exist or the App lost access"},
		{provider.ErrRateLimited, "rate limited", "rate limiting"},
		{provider.ErrUnavailable, "unavailable", "cannot reach the host"},
		{credential.ErrInsecureKeyFile, "key", "insecure private key file"},
		{token.ErrInsecureCache, "cache", "not private"},
		{token.ErrNoCacheDir, "cache", "no cache directory"},
		{token.ErrLockTimeout, "cache", "timed out"},
		{token.ErrCacheWrite, "cache", "cannot write"},
		{token.ErrUnusableToken, "cache", "unusable token"},
		{token.ErrInvalidKey, "cache", "invalid cache key"},
		{errors.New("something else"), "config", "something else"},
	}
	for _, tt := range tests {
		t.Run(tt.class+"/"+tt.hint, func(t *testing.T) {
			r := newRig(t, execContext)
			r.tokens.err = tt.err
			r.tokens.token = provider.Token{}
			out, code := r.run(t, false)
			want := "Token status:  unavailable (" + tt.class + ": "
			if !strings.Contains(out, want) || !strings.Contains(out, tt.hint) || code != 1 {
				t.Errorf("output %q exit %d, want %q… containing %q and exit 1", out, code, want, tt.hint)
			}
			js, _ := r.run(t, true)
			if !strings.Contains(js, `"class":"`+tt.class+`"`) || !strings.Contains(js, `"status":"unavailable"`) {
				t.Errorf("JSON = %s", js)
			}
		})
	}
}

func TestInstallationDeletedMessage(t *testing.T) {
	r := newRig(t, execContext)
	r.tokens.err = provider.ErrNotFound
	out, code := r.run(t, false)
	if !strings.Contains(out, "Token status:  unavailable (not found: the installation does not exist or the App lost access)\n") || code != 1 {
		t.Errorf("output %q exit %d", out, code)
	}
}

func TestKeyFailureOpeningTheProvider(t *testing.T) {
	r := newRig(t, execContext)
	r.h.OpenProvider = func(profile.Profile) (provider.Provider, error) {
		return nil, errors.Join(credential.ErrKeyNotFound, errors.New("/nowhere/key.pem"))
	}
	out, code := r.run(t, false)
	if !strings.Contains(out, "unavailable (key: ") || code != 1 {
		t.Errorf("output %q exit %d", out, code)
	}
}

func TestSecretsInErrorsAreRedacted(t *testing.T) {
	r := newRig(t, execContext)
	leak := testenv.FakeToken("leaked")
	r.tokens.err = errors.New("cannot write https://user:" + leak + "@host/ " + leak)
	out, _ := r.run(t, false)
	if strings.Contains(out, leak) || strings.Contains(out, "user:") {
		t.Errorf("output holds a secret: %q", out)
	}
}

func TestCommandFlagsAndArguments(t *testing.T) {
	r := newRig(t, Context{})
	for _, args := range [][]string{{"--bogus=" + testenv.FakeToken("x")}, {"extra"}} {
		cmd := NewCommand(r.h)
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stdout)
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		var exit *diagnose.ExitError
		if !errors.As(err, &exit) || exit.Code != 2 || strings.Contains(exit.Message, testenv.FakeTokenPrefix) {
			t.Errorf("%v: error %v, want exit 2 without the token", args, err)
		}
	}
	cmd := NewCommand(r.h)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil || !strings.Contains(stdout.String(), `"personal/unmanaged"`) {
		t.Errorf("whoami --json: %v, %q", err, stdout.String())
	}
}
