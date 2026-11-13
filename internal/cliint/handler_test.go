package cliint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

const personal = "personal-credential-value"

func TestHandleStartsTheRealCLIWithTheBotToken(t *testing.T) {
	f := newFixture(t)
	parent := f.parent(
		providertest.FakeCredentialVar+"="+personal, providertest.FakeCredentialVar+"_ALIAS="+personal,
		providertest.FakeConfigDirVar+"=/home/dev/.config/fake", "KEEP=yes",
	)
	if err := f.run([]string{"pr", "create", "--title", "x"}, parent); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(f.execer.calls) != 1 {
		t.Fatalf("%d starts, want 1", len(f.execer.calls))
	}
	call := f.execer.calls[0]
	if call.path != f.realPath {
		t.Errorf("path = %q, want the real CLI %q", call.path, f.realPath)
	}
	if got := strings.Join(call.argv, " "); got != providertest.FakeCLIBinary+" pr create --title x" {
		t.Errorf("argv = %q", got)
	}
	env := envOf(call)
	tokens := f.fake.Tokens()
	if len(tokens) != 1 || env[providertest.FakeCredentialVar] != tokens[0].Value {
		t.Errorf("%s = %q, want the minted token", providertest.FakeCredentialVar, env[providertest.FakeCredentialVar])
	}
	if _, ok := env[providertest.FakeCredentialVar+"_ALIAS"]; ok {
		t.Error("the alias credential variable survived")
	}
	if env[providertest.FakeConfigDirVar] != f.configD {
		t.Errorf("config dir = %q, want agent-id's %q", env[providertest.FakeConfigDirVar], f.configD)
	}
	if env[VarActive] != "1" || env["KEEP"] != "yes" {
		t.Errorf("marker %q, KEEP %q", env[VarActive], env["KEEP"])
	}
	for _, kv := range call.env {
		if strings.Contains(kv, personal) {
			t.Errorf("the personal value reached the CLI: %q", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if strings.Count(strings.Join(call.env, "\n"), providertest.FakeCredentialVar+"=") != 1 {
		t.Error("the credential variable is set more than once")
	}
}

func TestHandleGivesEachCallACurrentToken(t *testing.T) {
	f := newFixture(t)
	f.fake.SetTokenLifetime(time.Hour)
	f.clock.now = time.Now()
	for i := 0; i < 3; i++ {
		if err := f.run([]string{"pr", "view"}, f.parent()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if i == 1 {
			f.clock.now = f.clock.now.Add(56 * time.Minute)
			f.fake.SetTokenLifetime(2 * time.Hour)
		}
	}
	tokens := f.fake.Tokens()
	if len(tokens) != 2 {
		t.Fatalf("%d tokens minted, want 2 (one cached call, one refresh)", len(tokens))
	}
	got := []string{envOf(f.execer.calls[0])[providertest.FakeCredentialVar], envOf(f.execer.calls[1])[providertest.FakeCredentialVar], envOf(f.execer.calls[2])[providertest.FakeCredentialVar]}
	if got[0] != tokens[0].Value || got[1] != tokens[0].Value || got[2] != tokens[1].Value {
		t.Errorf("tokens per call = %v, want [first first second]", got)
	}
}

func TestHandleFailsClosedWhenTheMintFails(t *testing.T) {
	failures := []error{provider.ErrUnauthorized, provider.ErrForbidden, provider.ErrNotFound, provider.ErrUnavailable, provider.ErrRateLimited, errors.New("something unknown " + testenv.FakeToken("LEAK"))}
	for _, failure := range failures {
		t.Run(fmt.Sprint(failure)[:min(30, len(fmt.Sprint(failure)))], func(t *testing.T) {
			f := newFixture(t)
			f.fake.FailTokens(failure)
			err := f.run([]string{"pr", "create"}, f.parent(providertest.FakeCredentialVar+"="+personal))
			r := mustRefusal(t, err)
			if r.Code != "AID-FAKECLI-TOKEN" || !strings.Contains(r.Message, "fakecli was not started") {
				t.Errorf("refusal = %v", r)
			}
			assertClean(t, r.Error(), personal, testenv.FakeToken("LEAK"))
			if len(f.execer.calls) != 0 {
				t.Error("the CLI started after a failed mint")
			}
		})
	}
}

func TestHandleFailsClosedOnEveryOtherTokenProblem(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
	}{
		{"empty token from the manager", func(f *fixture) { f.handler.Tokens = fixedTokens{tok: provider.Token{}} }},
		{"token manager error", func(f *fixture) { f.handler.Tokens = fixedTokens{err: errors.New("cache broken " + personal)} }},
		{"provider cannot be opened", func(f *fixture) {
			f.handler.Provider = func(string) (provider.Provider, error) { return nil, errors.New("key file unreadable") }
		}},
		{"profile not ready", func(f *fixture) { f.act.ReadyError = errors.New("identity not resolved; run setup") }},
		{"provider environment without the token", func(f *fixture) {
			f.handler.Provider = func(string) (provider.Provider, error) { return noTokenEnv{f.fake}, nil }
		}},
		{"provider environment with a newline", func(f *fixture) {
			f.handler.Provider = func(string) (provider.Provider, error) { return newlineEnv{f.fake}, nil }
		}},
		{"provider environment names an agent-id variable", func(f *fixture) {
			f.handler.Provider = func(string) (provider.Provider, error) { return agentIDEnv{f.fake}, nil }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			r := mustRefusal(t, f.run([]string{"pr", "list"}, f.parent(providertest.FakeCredentialVar+"="+personal)))
			if r.Code != "AID-FAKECLI-TOKEN" {
				t.Errorf("code = %q", r.Code)
			}
			assertClean(t, r.Error(), personal)
			if len(f.execer.calls) != 0 {
				t.Error("the CLI started")
			}
		})
	}
}

func TestHandleMintFailureDoesNotUseTheCache(t *testing.T) {
	f := newFixture(t)
	if err := f.run([]string{"pr", "list"}, f.parent()); err != nil {
		t.Fatal(err)
	}
	f.clock.now = f.clock.now.Add(2 * time.Hour)
	f.fake.FailTokens(provider.ErrUnavailable)
	_ = mustRefusal(t, f.run([]string{"pr", "list"}, f.parent()))
	if len(f.execer.calls) != 1 {
		t.Errorf("%d starts, want only the first", len(f.execer.calls))
	}
}

func TestHandleRefusals(t *testing.T) {
	rule := &provider.CLIRefusal{Code: "AID-FAKECLI-AUTH", Message: "refused by the rules"}
	tests := []struct {
		name  string
		setup func(f *fixture)
		args  []string
		extra []string
		code  string
	}{
		{"deny-list", func(f *fixture) { f.act.CLIRule = func(_, _ []string) error { return rule } }, []string{"auth", "token"}, nil, "AID-FAKECLI-AUTH"},
		{"rules fail without a refusal", func(f *fixture) { f.act.CLIRule = func(_, _ []string) error { return errors.New("broken") } }, nil, nil, "AID-FAKECLI-CONTEXT"},
		{"marker set", nil, nil, []string{VarActive + "=1"}, "AID-FAKECLI-LOOP"},
		{"no real CLI", func(f *fixture) { _ = os.Remove(f.realPath) }, nil, nil, "AID-FAKECLI-NOCLI"},
		{"only a link to the launcher", func(f *fixture) {
			_ = os.Remove(f.realPath)
			if err := os.Symlink(filepath.Join(f.binDir, providertest.FakeCLIBinary), f.realPath); err != nil {
				t.Fatal(err)
			}
		}, nil, nil, "AID-FAKECLI-LOOP"},
		{"neutral directories fail", func(f *fixture) {
			f.handler.NeutralDirs = func([]string, provider.Activator) (string, string, error) { return "", "", errors.New("not private") }
		}, nil, nil, "AID-FAKECLI-CONTEXT"},
		{"no agent context", func(f *fixture) {
			f.handler.Profile = func([]string) (string, error) { return "", errors.New("no agent context in the environment") }
		}, nil, nil, "AID-FAKECLI-CONTEXT"},
		{"profile cannot be resolved", func(f *fixture) {
			f.handler.Activator = func(string) (provider.Activator, error) { return nil, errors.New("unknown profile") }
		}, nil, nil, "AID-FAKECLI-CONTEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			r := mustRefusal(t, f.run(tt.args, f.parent(tt.extra...)))
			if r.Code != tt.code {
				t.Errorf("code = %q, want %q (%s)", r.Code, tt.code, r.Message)
			}
			if len(f.execer.calls) != 0 {
				t.Error("the CLI started")
			}
			if len(f.fake.Tokens()) != 0 {
				t.Error("a token was minted for a refused call")
			}
		})
	}
}

func TestHandleRulesRunBeforeTheKeyIsLoaded(t *testing.T) {
	f := newFixture(t)
	f.act.CLIRule = func(_, _ []string) error { return &provider.CLIRefusal{Code: "AID-FAKECLI-USER", Message: "no"} }
	_ = mustRefusal(t, f.run([]string{"status"}, f.parent()))
	if f.opened != 0 {
		t.Error("the provider, and so the key, was opened for a refused command")
	}
}

func TestHandleRefusesAnotherBinary(t *testing.T) {
	f := newFixture(t)
	err := f.handler.Handle(t.Context(), Input{Binary: "othercli", Args: nil, Parent: f.parent()})
	if r := mustRefusal(t, err); r.Code != "AID-OTHERCLI-CONTEXT" {
		t.Errorf("code = %q", r.Code)
	}
}

func TestHandleReportsAFailedExec(t *testing.T) {
	f := newFixture(t)
	f.execer.err = errors.New("exec format error")
	r := mustRefusal(t, f.run(nil, f.parent()))
	if r.Code != "AID-FAKECLI-NOCLI" {
		t.Errorf("code = %q", r.Code)
	}
}

func TestHandleNeverPrintsTheToken(t *testing.T) {
	f := newFixture(t)
	if err := f.run([]string{"api", "repos/o/r"}, f.parent()); err != nil {
		t.Fatal(err)
	}
	for _, tok := range f.fake.Tokens() {
		for _, a := range f.execer.calls[0].argv {
			if strings.Contains(a, tok.Value) {
				t.Error("the token is in argv")
			}
		}
	}
}

func assertClean(t *testing.T, text string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Errorf("%q holds %q", text, s)
		}
	}
	if strings.Contains(text, testenv.FakeTokenPrefix) || strings.ContainsAny(text, "\n\r") {
		t.Errorf("%q holds a token prefix or a line break", text)
	}
}

type fixedTokens struct {
	tok provider.Token
	err error
}

func (f fixedTokens) Token(context.Context, token.Source) (provider.Token, error) {
	return f.tok, f.err
}

type noTokenEnv struct{ *providertest.Fake }

func (noTokenEnv) CLIEnvironment(provider.Token) []string {
	return []string{providertest.FakeCLIVar + "="}
}

type newlineEnv struct{ *providertest.Fake }

func (newlineEnv) CLIEnvironment(provider.Token) []string {
	return []string{providertest.FakeCLIVar + "=a\nB=c"}
}

type agentIDEnv struct{ *providertest.Fake }

func (agentIDEnv) CLIEnvironment(provider.Token) []string {
	return []string{"AGENT_ID_CONTEXT=1"}
}
