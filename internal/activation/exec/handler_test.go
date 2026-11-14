package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

type recorder struct {
	path      string
	argv, env []string
	err       error
	called    bool
}

func (r *recorder) Exec(path string, argv, env []string) error {
	r.called, r.path, r.argv, r.env = true, path, argv, env
	return r.err
}

func newHandler(t *testing.T) (*Handler, *recorder) {
	t.Helper()
	rec := &recorder{}
	return &Handler{
		Deps: activation.Deps{
			Profiles: profiles{}, Providers: source{providertest.NewFakeActivator()}, Neutral: neutral{t.TempDir()},
			Checks: func(provider.Activator) []activation.Check { return nil },
		},
		Execer: rec,
		Binary: func() (string, error) { return "/bin/agent-id", nil },
	}, rec
}

func input(argv ...string) Input {
	return Input{Profile: "claude", Argv: argv, Dir: "/", Parent: []string{"PATH=/bin:/usr/bin", "FAKE_CLI_TOKEN=" + testenv.FakeToken("personal")}}
}

func code(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 0
}

func TestHandleExecsWithTheChildEnvironment(t *testing.T) {
	testenv.Isolate(t)
	h, rec := newHandler(t)
	if err := h.Handle(context.Background(), input("sh", "-c", "true")); err != nil {
		t.Fatal(err)
	}
	if !rec.called || filepath.Base(rec.path) != "sh" || strings.Join(rec.argv, " ") != "sh -c true" {
		t.Errorf("exec %q %v", rec.path, rec.argv)
	}
	for _, kv := range rec.env {
		if strings.Contains(kv, testenv.FakeToken("personal")) {
			t.Errorf("the personal token reached the command")
		}
	}
	if !contains(rec.env, "AGENT_ID_ACTIVE_PROFILE=claude") || !contains(rec.env, "AGENT_ID_MECHANISM=exec") {
		t.Errorf("context missing: %v", rec.env)
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func TestHandleFailures(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name   string
		mutate func(h *Handler, in *Input)
		code   int
		msg    string
	}{
		{"profile required", func(_ *Handler, in *Input) { in.Profile = "" }, 125, "--profile is required"},
		{"no command", func(_ *Handler, in *Input) { in.Argv = nil }, 125, "no command given"},
		{"binary path", func(h *Handler, _ *Input) { h.Binary = func() (string, error) { return "", activation.ErrBinaryPath } }, 125, ""},
		{"profile error", func(h *Handler, _ *Input) { h.Deps.Profiles = profiles{err: profile.ErrProfileNotFound} }, 125, ""},
		{"not found", func(_ *Handler, in *Input) { in.Argv = []string{"does-not-exist"} }, 127, `"does-not-exist": command not found`},
		{"not executable", func(_ *Handler, in *Input) { in.Argv = []string{"/etc/hosts"} }, 126, "permission denied"},
		{"execve fails with EACCES", func(h *Handler, _ *Input) {
			h.Execer = &recorder{err: &os.PathError{Op: "fork/exec", Path: "x", Err: syscall.EACCES}}
		}, 126, "permission denied"},
		{"execve fails with ENOENT", func(h *Handler, _ *Input) { h.Execer = &recorder{err: syscall.ENOENT} }, 127, "interpreter or loader not found"},
		{"empty drop list", func(h *Handler, _ *Input) {
			h.Deps.Providers = source{&providertest.FakeActivator{CLI: providertest.NewFakeActivator().CLI, Ident: providertest.NewFakeActivator().Ident}}
		}, 125, "plan contains"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, rec := newHandler(t)
			in := input("sh")
			tt.mutate(h, &in)
			err := h.Handle(context.Background(), in)
			if err == nil || code(err) != tt.code || !strings.HasPrefix(err.Error(), "exec: ") || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("error %v (code %d), want %d containing %q", err, code(err), tt.code, tt.msg)
			}
			if rec.called {
				t.Error("the command ran")
			}
		})
	}
}

func (n neutral) EnsureSession([]string, activation.NeutralPaths, provider.NeutralCLI) (string, error) {
	return n.dir + "/claude-env.sh", nil
}
