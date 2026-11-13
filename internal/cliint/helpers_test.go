package cliint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type execCall struct {
	path      string
	argv, env []string
}

type recordingExecer struct {
	calls []execCall
	err   error
}

func (r *recordingExecer) Exec(path string, argv, env []string) error {
	r.calls = append(r.calls, execCall{path, argv, env})
	return r.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type fixture struct {
	t        *testing.T
	env      testenv.Env
	fake     *providertest.Fake
	act      *providertest.FakeActivator
	clock    *clock
	execer   *recordingExecer
	handler  *Handler
	binDir   string
	configD  string
	realDir  string
	realPath string
	opened   int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := testenv.Isolate(t)
	f := &fixture{t: t, env: env, fake: providertest.NewFake(), act: providertest.NewFakeActivator(), clock: &clock{now: time.Now()}, execer: &recordingExecer{}}
	f.binDir = filepath.Join(env.TempDir, "neutral-bin")
	f.configD = filepath.Join(env.TempDir, "cli-config")
	f.realDir = filepath.Join(env.TempDir, "real")
	for _, d := range []string{f.binDir, f.configD, f.realDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, filepath.Join(f.binDir, providertest.FakeCLIBinary), "#!/bin/sh\nexit 1\n")
	f.realPath = filepath.Join(f.realDir, providertest.FakeCLIBinary)
	writeExec(t, f.realPath, "#!/bin/sh\nexit 0\n")
	dir, err := token.Dir(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	f.handler = &Handler{
		Profile:     func([]string) (string, error) { return "claude", nil },
		Activator:   func(string) (provider.Activator, error) { return f.act, nil },
		Provider:    func(string) (provider.Provider, error) { f.opened++; return f.fake, nil },
		NeutralDirs: func([]string, provider.Activator) (string, string, error) { return f.configD, f.binDir, nil },
		Tokens:      &token.Manager{Dir: dir, Now: f.clock.Now},
		Execer:      f.execer,
	}
	return f
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

// parent returns an environment whose PATH has the launcher's directory
// first and the real CLI's second, as exec arranges.
func (f *fixture) parent(extra ...string) []string {
	p := []string{"PATH=" + f.binDir + ":" + f.realDir + ":/usr/bin", "HOME=" + f.env.Home}
	return append(p, extra...)
}

func (f *fixture) run(args []string, parent []string) error {
	f.t.Helper()
	return f.handler.Handle(context.Background(), Input{Binary: providertest.FakeCLIBinary, Args: args, Parent: parent})
}

func envOf(call execCall) map[string]string {
	out := map[string]string{}
	for _, kv := range call.env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}

func mustRefusal(t *testing.T, err error) *Refusal {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("error = %v, want a *Refusal", err)
	}
	if r.ExitCode() != ExitRefused {
		t.Errorf("exit code %d, want %d", r.ExitCode(), ExitRefused)
	}
	return r
}
