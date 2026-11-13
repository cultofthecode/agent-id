package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestReadContext(t *testing.T) {
	valid := map[string]string{
		"AGENT_ID_CONTEXT": "1", "AGENT_ID_ACTIVE_PROFILE": "claude", "AGENT_ID_MECHANISM": "exec",
		"AGENT_ID_GIT_CONFIG_BASE": "0", "AGENT_ID_GIT_CONFIG_END": "3", "GIT_CONFIG_COUNT": "3",
	}
	with := func(changes map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range valid {
			m[k] = v
		}
		for k, v := range changes {
			if v == "<unset>" {
				delete(m, k)
				continue
			}
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name        string
		env         map[string]string
		present     bool
		problem     string
		profile     string
		mechanism   string
		problemPart string
	}{
		{"no context", map[string]string{"AGENT_ID_ACTIVE_PROFILE": "claude"}, false, "", "", "", ""},
		{"valid exec", valid, true, "", "claude", "exec", ""},
		{"valid env", with(map[string]string{"AGENT_ID_MECHANISM": "env"}), true, "", "claude", "env", ""},
		{"future version", with(map[string]string{"AGENT_ID_CONTEXT": "2"}), true, "incompatible context version 2", "claude", "exec", ""},
		{"empty version", with(map[string]string{"AGENT_ID_CONTEXT": ""}), true, "incompatible context version ", "claude", "exec", ""},
		{"version with a space", with(map[string]string{"AGENT_ID_CONTEXT": "1 "}), true, "incompatible context version 1 ", "claude", "exec", ""},
		{"missing profile", with(map[string]string{"AGENT_ID_ACTIVE_PROFILE": "<unset>"}), true, "", "", "exec", "AGENT_ID_ACTIVE_PROFILE"},
		{"uppercase profile", with(map[string]string{"AGENT_ID_ACTIVE_PROFILE": "Claude"}), true, "", "Claude", "exec", "AGENT_ID_ACTIVE_PROFILE"},
		{"profile with metacharacters", with(map[string]string{"AGENT_ID_ACTIVE_PROFILE": "a;b"}), true, "", "a;b", "exec", "AGENT_ID_ACTIVE_PROFILE"},
		{"33-character profile", with(map[string]string{"AGENT_ID_ACTIVE_PROFILE": strings.Repeat("a", 33)}), true, "", strings.Repeat("a", 33), "exec", "AGENT_ID_ACTIVE_PROFILE"},
		{"unknown mechanism", with(map[string]string{"AGENT_ID_MECHANISM": "shell"}), true, "", "claude", "shell", "AGENT_ID_MECHANISM"},
		{"base not an integer", with(map[string]string{"AGENT_ID_GIT_CONFIG_BASE": "x"}), true, "", "claude", "exec", "GIT_CONFIG_BASE"},
		{"base negative", with(map[string]string{"AGENT_ID_GIT_CONFIG_BASE": "-1"}), true, "", "claude", "exec", "GIT_CONFIG_BASE"},
		{"base above end", with(map[string]string{"AGENT_ID_GIT_CONFIG_BASE": "4"}), true, "", "claude", "exec", "GIT_CONFIG_BASE"},
		{"end missing", with(map[string]string{"AGENT_ID_GIT_CONFIG_END": "<unset>"}), true, "", "claude", "exec", "GIT_CONFIG_END"},
		{"count unset but end positive", with(map[string]string{"GIT_CONFIG_COUNT": "<unset>"}), true, "", "claude", "exec", "changed after activation"},
		{"count differs", with(map[string]string{"GIT_CONFIG_COUNT": "4"}), true, "", "claude", "exec", "changed after activation"},
		{"count not an integer", with(map[string]string{"GIT_CONFIG_COUNT": "many"}), true, "", "claude", "exec", "GIT_CONFIG_COUNT"},
		{"count unset and end zero", with(map[string]string{"GIT_CONFIG_COUNT": "<unset>", "AGENT_ID_GIT_CONFIG_END": "0"}), true, "", "claude", "exec", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readContext(func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok })
			if got.Present != tt.present || got.Profile != tt.profile || got.Mechanism != tt.mechanism {
				t.Fatalf("readContext = %+v, want present %v profile %q mechanism %q", got, tt.present, tt.profile, tt.mechanism)
			}
			switch {
			case tt.problem != "":
				if got.Problem != tt.problem {
					t.Errorf("problem = %q, want %q", got.Problem, tt.problem)
				}
			case tt.problemPart != "":
				if !strings.HasPrefix(got.Problem, "damaged: ") || !strings.Contains(got.Problem, tt.problemPart) {
					t.Errorf("problem = %q, want damaged with %q", got.Problem, tt.problemPart)
				}
			case got.Problem != "":
				t.Errorf("problem = %q, want none", got.Problem)
			}
		})
	}
}

// runBinary runs the binary with args in env and returns stdout, stderr, and
// the exit code.
func runBinary(t *testing.T, binary string, env []string, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// recordingTools writes fake git and gh programs that record each call, and
// returns the directory for PATH and the call log.
func recordingTools(t *testing.T, env testenv.Env) (dir, log string) {
	t.Helper()
	dir = filepath.Join(env.Root, "fakebin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(env.Root, "calls.log")
	script := "#!/bin/sh\necho \"$0 $*\" >> " + log + "\nexit 0\n"
	for _, name := range []string{"git", "gh", "ssh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir, log
}

func TestDiagnosticCommandsInAProcess(t *testing.T) {
	env := testenv.Isolate(t)
	binary := testenv.BuildBinary(t, env, "github.com/cultofthecode/agent-id/cmd/agent-id")
	fakebin, calls := recordingTools(t, env)
	decoyToken := testenv.FakeToken("process")
	decoyReal := "ghs_" + "processdecoy1234567890"
	base := append(os.Environ(),
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"GH_TOKEN="+decoyToken, "HOMEBREW_GITHUB_API_TOKEN="+decoyReal,
	)

	t.Run("whoami outside a context", func(t *testing.T) {
		for _, args := range [][]string{{"whoami"}, {"whoami", "--json"}} {
			stdout, stderr, code := runBinary(t, binary, base, args...)
			want := "Context:       personal/unmanaged\n"
			if len(args) == 2 {
				want = `{"context":"personal/unmanaged","schema":1}` + "\n"
			}
			if stdout != want || stderr != "" || code != 0 {
				t.Errorf("%v: stdout %q stderr %q exit %d; want %q and exit 0", args, stdout, stderr, code, want)
			}
		}
		if data, err := os.ReadFile(calls); err == nil {
			t.Errorf("whoami outside a context started a child process:\n%s", data)
		}
	})

	t.Run("whoami in a damaged context", func(t *testing.T) {
		stdout, _, code := runBinary(t, binary, append(base, "AGENT_ID_CONTEXT=2"), "whoami")
		if stdout != "Context:       agent (incompatible context version 2)\nToken status:  not checked (damaged context)\n" || code != 1 {
			t.Errorf("stdout %q exit %d", stdout, code)
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		for _, args := range [][]string{{"doctor"}, {"doctor", "--profile=x", "--nope=" + decoyReal}, {"whoami", "extra"}, {"whoami", "--bogus=" + decoyToken}} {
			stdout, stderr, code := runBinary(t, binary, base, args...)
			if code != 2 || stdout != "" || strings.Contains(stderr, decoyReal) || strings.Contains(stderr, decoyToken) {
				t.Errorf("%v: stdout %q stderr %q exit %d; want exit 2 and no token", args, stdout, stderr, code)
			}
		}
	})

	t.Run("doctor with no configuration", func(t *testing.T) {
		for _, args := range [][]string{{"doctor", "--profile", "claude"}, {"doctor", "--profile", "claude", "--json"}} {
			stdout, stderr, code := runBinary(t, binary, base, args...)
			if code != 1 || !strings.Contains(stdout, "config.load") || stderr != "" {
				t.Errorf("%v: stdout %q stderr %q exit %d", args, stdout, stderr, code)
			}
			for _, decoy := range []string{decoyToken, decoyReal, env.Home} {
				if strings.Contains(stdout, decoy) {
					t.Errorf("%v: the output holds %q:\n%s", args, decoy, stdout)
				}
			}
			if !strings.Contains(stdout, "GH_TOKEN") || !strings.Contains(stdout, "HOMEBREW_GITHUB_API_TOKEN") {
				t.Errorf("%v: the decoy variables must be reported by name:\n%s", args, stdout)
			}
		}
	})
}
