package env

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/activation"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type profiles struct{ err error }

func (p profiles) CheckName(string) error { return nil }
func (p profiles) Resolve(n string) (profile.Profile, error) {
	return profile.Profile{Name: n, Provider: "fake", Host: providertest.FakeHost}, p.err
}

type source struct{ a *providertest.FakeActivator }

func (s source) Activator(profile.Profile) (provider.Activator, error) { return s.a, nil }

type neutral struct{ dir string }

func (n neutral) Ensure([]string, string, provider.NeutralCLI) (activation.NeutralPaths, error) {
	return activation.NeutralPaths{ConfigDir: n.dir, BinDir: n.dir}, nil
}

func newHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		Deps: activation.Deps{
			Profiles: profiles{}, Providers: source{providertest.NewFakeActivator()}, Neutral: neutral{t.TempDir()},
			Checks: func(provider.Activator) []activation.Check { return nil },
		},
		Binary: func() (string, error) { return "/bin/agent-id", nil },
	}
}

func input() Input {
	return Input{Profile: "claude", Shell: "zsh", Dir: "/", Parent: []string{"PATH=/bin:/usr/bin", "FAKE_CLI_TOKEN=" + testenv.FakeToken("personal")}}
}

func code(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 0
}

func TestHandlePrintsTheBlockWithoutSecret(t *testing.T) {
	testenv.Isolate(t)
	block, err := newHandler(t).Handle(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# agent-id env 1 profile=claude", "unset FAKE_CLI_TOKEN", "export AGENT_ID_MECHANISM='env'", "export AGENT_ID_ACTIVE_PROFILE='claude'"} {
		if !strings.Contains(block, want) {
			t.Errorf("missing %q in\n%s", want, block)
		}
	}
	for _, bad := range []string{testenv.FakeToken("personal"), testenv.FakeTokenPrefix, "-----BEGIN"} {
		if strings.Contains(block, bad) {
			t.Errorf("block contains %q", bad)
		}
	}
}

func TestHandleFailuresGive125AndNoBlock(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name   string
		mutate func(h *Handler, in *Input)
		msg    string
	}{
		{"profile required", func(_ *Handler, in *Input) { in.Profile = "" }, "--profile is required"},
		{"shell required", func(_ *Handler, in *Input) { in.Shell = "" }, "--shell is required"},
		{"fish", func(_ *Handler, in *Input) { in.Shell = "fish" }, `unsupported shell "fish"; use bash or zsh`},
		{"binary path", func(h *Handler, _ *Input) { h.Binary = func() (string, error) { return "", activation.ErrBinaryPath } }, ""},
		{"profile error", func(h *Handler, _ *Input) { h.Deps.Profiles = profiles{err: profile.ErrProfileNotFound} }, ""},
		{"different profile inside a session", func(_ *Handler, in *Input) {
			in.Profile = "reviewer"
			in.Parent = append(in.Parent, "AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0")
		}, `"claude"`},
		{"identity unavailable", func(h *Handler, _ *Input) {
			a := providertest.NewFakeActivator()
			a.IdentityError = errors.New("no cache")
			h.Deps.Providers = source{a}
		}, "cannot read the bot identity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, in := newHandler(t), input()
			tt.mutate(h, &in)
			block, err := h.Handle(context.Background(), in)
			if err == nil || code(err) != 125 || block != "" || !strings.HasPrefix(err.Error(), "env: ") || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("err=%v code=%d block=%q", err, code(err), block)
			}
		})
	}
	_, err := newHandler(t).Handle(context.Background(), func() Input {
		i := input()
		i.Profile = "reviewer"
		i.Parent = append(i.Parent, "AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0")
		return i
	}())
	if err == nil || !strings.Contains(err.Error(), "reviewer") {
		t.Fatalf("nested error must name both profiles: %v", err)
	}
}
